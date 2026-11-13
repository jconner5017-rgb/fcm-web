// Package email sends FCM Web's account mail (confirm your email, password
// reset) through Postmark, Resend or Mailjet. Copied from Slabs & Singles,
// without the staff email blasts.
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// Message is one email.
type Message struct {
	To, Subject, Text string
	Broadcast         bool              // a blast (marketing stream), not an account email
	Headers           map[string]string // e.g. List-Unsubscribe
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, from string, m Message) error
	Name() string
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func post(ctx context.Context, url string, hdr map[string]string, body any) error {
	_, err := postRead(ctx, url, hdr, body)
	return err
}

// postRead is post that also returns the response body.
func postRead(ctx context.Context, url string, hdr map[string]string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 20000))
	if res.StatusCode >= 300 {
		return data, fmt.Errorf("HTTP %d: %s", res.StatusCode, trim(strings.TrimSpace(string(data)), 2000))
	}
	return data, nil
}

// Postmark sends with a server API token. Blasts use its "broadcast"
// message stream, kept apart from account email as Postmark requires.
type Postmark struct{ Token string }

func (p Postmark) Name() string { return "postmark" }

func (p Postmark) Send(ctx context.Context, from string, m Message) error {
	stream := "outbound"
	if m.Broadcast {
		stream = "broadcast"
	}
	var hs []map[string]string
	for k, v := range m.Headers {
		hs = append(hs, map[string]string{"Name": k, "Value": v})
	}
	return post(ctx, "https://api.postmarkapp.com/email", map[string]string{"X-Postmark-Server-Token": p.Token},
		map[string]any{"From": from, "To": m.To, "Subject": m.Subject, "TextBody": m.Text, "MessageStream": stream, "Headers": hs})
}

// Resend sends with an API key.
type Resend struct{ Key string }

func (r Resend) Name() string { return "resend" }

func (r Resend) Send(ctx context.Context, from string, m Message) error {
	body := map[string]any{"from": from, "to": []string{m.To}, "subject": m.Subject, "text": m.Text}
	if len(m.Headers) > 0 {
		body["headers"] = m.Headers
	}
	return post(ctx, "https://api.resend.com/emails", map[string]string{"Authorization": "Bearer " + r.Key}, body)
}

// Mailjet sends with an API key and secret key (Send API v3.1).
type Mailjet struct{ Key, Secret string }

func (j Mailjet) Name() string { return "mailjet" }

func (j Mailjet) Send(ctx context.Context, from string, m Message) error {
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("EMAIL_FROM: %w", err)
	}
	msg := map[string]any{
		"From":     map[string]string{"Email": addr.Address, "Name": addr.Name},
		"To":       []map[string]string{{"Email": m.To}},
		"Subject":  m.Subject,
		"TextPart": m.Text,
	}
	if len(m.Headers) > 0 {
		msg["Headers"] = m.Headers
	}
	auth := base64.StdEncoding.EncodeToString([]byte(j.Key + ":" + j.Secret))
	data, err := postRead(ctx, "https://api.mailjet.com/v3.1/send", map[string]string{"Authorization": "Basic " + auth},
		map[string]any{"Messages": []any{msg}})
	if err != nil {
		return err
	}
	// Mailjet answers per message; anything but "success" is a failure.
	var r struct {
		Messages []struct {
			Status string
			Errors []struct{ ErrorMessage, ErrorCode string }
		}
	}
	if json.Unmarshal(data, &r) == nil && len(r.Messages) > 0 && r.Messages[0].Status != "success" {
		var why []string
		for _, e := range r.Messages[0].Errors {
			why = append(why, e.ErrorCode+" "+e.ErrorMessage)
		}
		return fmt.Errorf("mailjet: %s %s", r.Messages[0].Status, strings.Join(why, "; "))
	}
	return nil
}

// Pick builds a sender from EMAIL_PROVIDER, EMAIL_API_KEY and (Mailjet)
// EMAIL_API_SECRET; nil when unset.
func Pick(provider, key, secret string) (Sender, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "none":
		return nil, nil
	case "postmark":
		if key == "" {
			return nil, errors.New("EMAIL_PROVIDER=postmark needs EMAIL_API_KEY (a Postmark server token)")
		}
		return Postmark{Token: key}, nil
	case "resend":
		if key == "" {
			return nil, errors.New("EMAIL_PROVIDER=resend needs EMAIL_API_KEY")
		}
		return Resend{Key: key}, nil
	case "mailjet":
		if key == "" || secret == "" {
			return nil, errors.New("EMAIL_PROVIDER=mailjet needs EMAIL_API_KEY and EMAIL_API_SECRET")
		}
		return Mailjet{Key: key, Secret: secret}, nil
	}
	return nil, errors.New("EMAIL_PROVIDER must be postmark, resend or mailjet")
}

// Service queues and sends account mail through the email_outbox table, so a
// slow or failing provider never holds up a request.
type Service struct {
	DB     *sql.DB
	Sender Sender                        // nil: nothing is sent (local and tests)
	From   string                        // e.g. "FireCardManager <hello@firecardmanager.com>"
	WebURL string                        // https://app.firecardmanager.com
	Warn   func(msg string, args ...any) // logs failed sends (optional)
	Now    func() time.Time
}

// Enabled reports whether mail actually goes out.
func (s *Service) Enabled() bool { return s != nil && s.Sender != nil }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// Queue adds an account email to the outbox.
func (s *Service) Queue(ctx context.Context, q execer, accountID, to, subject, text string) error {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(to)), ".invalid") {
		return nil
	}
	_, err := q.ExecContext(ctx, `INSERT INTO email_outbox (account_id, to_email, subject, body) VALUES (NULLIF($1, '')::uuid, $2, $3, $4)`,
		accountID, to, subject, text)
	return err
}

// RunOutbox sends up to batch queued messages. Failures retry with a
// growing delay, five tries in all.
func (s *Service) RunOutbox(ctx context.Context, batch int) (sent int, err error) {
	if !s.Enabled() {
		return 0, nil
	}
	type job struct {
		id                int64
		to, subject, body string
		attempts          int
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, to_email, subject, body, attempts FROM email_outbox
		WHERE status = 'queued' AND send_after <= now() ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, batch)
	if err != nil {
		return 0, err
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.to, &j.subject, &j.body, &j.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	for _, j := range jobs {
		if serr := s.Sender.Send(ctx, s.From, Message{To: j.to, Subject: j.subject, Text: j.body}); serr != nil {
			if s.Warn != nil {
				s.Warn("email not sent", "to", j.to, "subject", j.subject, "try", j.attempts+1, "err", serr)
			}
			status := "queued"
			if j.attempts+1 >= 5 {
				status = "failed"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE email_outbox SET attempts = attempts + 1, last_error = $2, status = $3,
				send_after = now() + make_interval(mins => 5 * (attempts + 1)) WHERE id = $1`, j.id, trim(serr.Error(), 500), status); err != nil {
				return sent, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE email_outbox SET status = 'sent', sent_at = now(), attempts = attempts + 1, last_error = NULL WHERE id = $1`, j.id); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, tx.Commit()
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---- Single-use tokens: confirm an email, reset a password ----

var ErrToken = errors.New("that link has expired or was already used")

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// NewToken stores a single-use token for an account and returns it.
func (s *Service) NewToken(ctx context.Context, q execer, accountID, email, purpose string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	_, err := q.ExecContext(ctx, `INSERT INTO email_tokens (token_hash, account_id, purpose, email, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		hashToken(t), accountID, purpose, email, s.now().Add(ttl))
	return t, err
}

// UseToken spends a token and returns whose it was and the email it was sent to.
func (s *Service) UseToken(ctx context.Context, tx *sql.Tx, token, purpose string) (accountID, email string, err error) {
	err = tx.QueryRowContext(ctx, `UPDATE email_tokens SET used_at = now()
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3
		RETURNING account_id, email`, hashToken(token), purpose, s.now()).Scan(&accountID, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrToken
	}
	return accountID, email, err
}

// RecentTokens counts tokens of a kind made for an account in the last
// window, to limit resends.
func (s *Service) RecentTokens(ctx context.Context, accountID, purpose string, window time.Duration) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM email_tokens WHERE account_id = $1 AND purpose = $2 AND created_at > $3`,
		accountID, purpose, s.now().Add(-window)).Scan(&n)
	return n, err
}
