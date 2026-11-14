package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sign-up bot protection: a hidden field people never fill in, a signed
// form token (a bot posting straight to the API, or faster than a person
// can type, is turned away), throwaway email services, a cap on accounts
// per network per day, and Cloudflare Turnstile when its keys are set.

// disposableDomains are throwaway inbox services.
var disposableDomains = map[string]bool{}

func init() {
	for _, d := range strings.Fields(`mailinator.com guerrillamail.com guerrillamail.net guerrillamail.org guerrillamailblock.com sharklasers.com grr.la
		10minutemail.com 10minutemail.net 20minutemail.com tempmail.com temp-mail.org temp-mail.io tempmail.net tempmailo.com tempr.email tempail.com
		yopmail.com yopmail.net yopmail.fr trashmail.com trashmail.de trashmail.net getnada.com nada.email dispostable.com maildrop.cc mailnesia.com
		throwawaymail.com fakeinbox.com mohmal.com emailondeck.com mintemail.com burnermail.io spamgourmet.com moakt.com mailcatch.com
		mytemp.email tempinbox.com discard.email discardmail.com spambox.us getairmail.com inboxbear.com mailpoof.com emailfake.com
		fakemail.net 1secmail.com 1secmail.org 1secmail.net mail.tm mail.gw byom.de harakirimail.com mailsac.com spam4.me 33mail.com
		anonaddy.me tmail.ws tmpmail.org tmpmail.net linshiyouxiang.net dropmail.me minuteinbox.com emailtemporanea.net`) {
		disposableDomains[d] = true
	}
}

func disposable(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	d := strings.ToLower(strings.TrimSpace(email[at+1:]))
	for d != "" {
		if disposableDomains[d] {
			return true
		}
		i := strings.IndexByte(d, '.')
		if i < 0 {
			break
		}
		d = d[i+1:]
	}
	return false
}

func (s *Server) formToken(now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	m := hmac.New(sha256.New, s.formKey)
	m.Write([]byte("signup|" + ts))
	return ts + "." + hex.EncodeToString(m.Sum(nil))
}

// formAge checks a token from formToken and returns how old it is.
func (s *Server) formAge(tok string, now time.Time) (time.Duration, bool) {
	ts, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return 0, false
	}
	m := hmac.New(sha256.New, s.formKey)
	m.Write([]byte("signup|" + ts))
	want := hex.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return 0, false
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 0, false
	}
	return now.Sub(time.Unix(n, 0)), true
}

// handleSignupCheck gives the sign-up form its token and the Turnstile site key.
func (s *Server) handleSignupCheck(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"turnstile_site_key": s.turnstileSiteKey}
	if len(s.formKey) > 0 {
		out["form_token"] = s.formToken(s.now())
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

type signupBlock struct {
	status  int
	code    string
	reason  string // recorded: signup_blocked:<reason>
	message string
}

// signupGuard returns nil when the sign-up may go ahead.
func (s *Server) signupGuard(r *http.Request, email, website, formTok, turnstile string) *signupBlock {
	bot := &signupBlock{http.StatusBadRequest, "bot_check", "", "We couldn't confirm you're a person. Reload the page and try again."}
	if strings.TrimSpace(website) != "" {
		bot.reason = "honeypot"
		return bot
	}
	if len(s.formKey) > 0 {
		age, ok := s.formAge(formTok, s.now())
		switch {
		case !ok:
			bot.reason = "no_form"
			return bot
		case age < 3*time.Second:
			bot.reason = "too_fast"
			return bot
		case age > 6*time.Hour:
			return &signupBlock{http.StatusBadRequest, "form_expired", "form_expired", "This page was open too long. Reload it and try again."}
		}
	}
	if disposable(email) {
		return &signupBlock{http.StatusUnprocessableEntity, "disposable_email", "disposable",
			"Use an email address you'll keep. Throwaway inboxes can't be used for an account."}
	}
	ip := clientIP(r, s.trustProxy)
	if s.signupsPerIPDay > 0 && ip != "" {
		var n int
		s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM accounts WHERE signup_ip = $1 AND created_at > now() - interval '1 day'`, ip).Scan(&n)
		if n >= s.signupsPerIPDay {
			return &signupBlock{http.StatusTooManyRequests, "too_many_signups", "ip_limit",
				"Too many accounts have been made from this network today. Try again tomorrow, or contact FireCardManager support."}
		}
	}
	if s.turnstileSecret != "" {
		if ok, err := s.verifyTurnstile(r.Context(), turnstile, ip); err != nil {
			s.log.Warn("turnstile", "err", err) // Cloudflare unreachable: don't lock people out
		} else if !ok {
			bot.reason = "turnstile"
			return bot
		}
	}
	return nil
}

func (s *Server) verifyTurnstile(ctx context.Context, token, ip string) (bool, error) {
	if token == "" {
		return false, nil
	}
	form := url.Values{"secret": {s.turnstileSecret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.turnstileURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("turnstile reply: %w", err)
	}
	return out.Success, nil
}

func (s *Server) signupEvent(ctx context.Context, accountID any, email, ip, ua string, ok bool, reason string) {
	if len(ua) > 300 {
		ua = ua[:300]
	}
	s.db.ExecContext(ctx, `INSERT INTO login_events (account_id, email, ip, user_agent, success, reason) VALUES ($1, $2, $3, $4, $5, $6)`,
		accountID, email, ip, ua, ok, reason)
}
