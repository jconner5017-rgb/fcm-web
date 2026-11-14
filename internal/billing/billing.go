// Package billing sells FireCardManager Web plans through FireCardManager's
// own payment system: firecardmanager.com/checkout.php (Stripe Checkout on
// the Fire Card Manager Stripe account, the same page the desktop Register
// window opens). stripe-webhook.php there emails the owner every sale (and
// for a bundle, the Hardware ID to issue the desktop key); this service's
// webhook, a second endpoint on the same Stripe account, adds the months to
// the account automatically.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Plan is one thing the Account page sells.
type Plan struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Plan   string `json:"plan"` // accounts.web_plan: web or bundle
	Months int    `json:"months"`
	Cents  int    `json:"cents"`
}

// Plans and prices (decided 2026-11-13). The desktop license alone stays on
// checkout.php ($39.99 / 6 months, $59.99 / 12 months).
var Plans = []Plan{
	{"web6", "FireCardManager Web - 6 months", "web", 6, 3999},
	{"web12", "FireCardManager Web - 12 months", "web", 12, 5999},
	{"bundle6", "FireCardManager + FireCardManager Web bundle - 6 months", "bundle", 6, 5999},
	{"bundle12", "FireCardManager + FireCardManager Web bundle - 12 months", "bundle", 12, 8999},
}

func PlanByKey(k string) (Plan, bool) {
	for _, p := range Plans {
		if p.Key == k {
			return p, true
		}
	}
	return Plan{}, false
}

var (
	ErrNotConfigured = errors.New("online payment isn't switched on")
	ErrBadSignature  = errors.New("bad stripe signature")
)

type Service struct {
	DB            *sql.DB
	WebhookSecret string // STRIPE_WEBHOOK_SECRET: this endpoint's signing secret in the Fire Card Manager Stripe account
	CheckoutPage  string // default https://firecardmanager.com/checkout.php
}

// Configured: paid plans are added automatically (the webhook is set up).
// Without it, buying still works and the owner adds the months from the
// purchase email, as with desktop licenses.
func (s *Service) Configured() bool { return s != nil && s.WebhookSecret != "" }

// CheckoutURL is FireCardManager's own checkout page (firecardmanager.com/
// checkout.php, the one the desktop Register window uses) for a plan: it
// opens Stripe Checkout on the Fire Card Manager Stripe account with the
// account id (and for a bundle the PC's Hardware ID) attached.
func (s *Service) CheckoutURL(accountID, planKey, hwid string) (string, error) {
	if _, ok := PlanByKey(planKey); !ok {
		return "", fmt.Errorf("unknown plan %q", planKey)
	}
	q := url.Values{"app": {"web"}, "plan": {planKey}, "account": {accountID}}
	if hwid != "" {
		q.Set("hwid", hwid)
	}
	base := s.CheckoutPage
	if base == "" {
		base = "https://firecardmanager.com/checkout.php"
	}
	return base + "?" + q.Encode(), nil
}

// VerifySignature checks a Stripe-Signature header (t=..., v1=...) against the payload.
func VerifySignature(payload []byte, header, secret string, now time.Time) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrBadSignature
	}
	if d := now.Sub(time.Unix(n, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return ErrBadSignature
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(payload)
	want := hex.EncodeToString(m.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return ErrBadSignature
}

// Paid is a completed FireCardManager Web checkout from the webhook.
type Paid struct {
	SessionID string
	AccountID string
	Plan      Plan
	Cents     int
	Email     string
	HWID      string
}

// ParseEvent reads a webhook event; ok is false for anything that isn't a
// paid FireCardManager Web checkout (license sales from checkout.php share
// the Stripe account and are left alone).
func ParseEvent(payload []byte) (Paid, bool, error) {
	var ev struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID            string            `json:"id"`
				PaymentStatus string            `json:"payment_status"`
				AmountTotal   int               `json:"amount_total"`
				Metadata      map[string]string `json:"metadata"`
				Customer      struct {
					Email string `json:"email"`
				} `json:"customer_details"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return Paid{}, false, err
	}
	o := ev.Data.Object
	if (ev.Type != "checkout.session.completed" && ev.Type != "checkout.session.async_payment_succeeded") ||
		o.PaymentStatus != "paid" || o.Metadata["fcmweb"] != "1" {
		return Paid{}, false, nil
	}
	p, ok := PlanByKey(o.Metadata["plan_key"])
	if !ok || o.Metadata["account_id"] == "" {
		return Paid{}, false, nil
	}
	return Paid{SessionID: o.ID, AccountID: o.Metadata["account_id"], Plan: p, Cents: o.AmountTotal, Email: o.Customer.Email, HWID: o.Metadata["hwid"]}, true, nil
}

// Record stores a payment; false when this session was already recorded.
func (s *Service) Record(ctx context.Context, p Paid) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO billing_payments (session_id, account_id, plan, months, amount_cents, email, hwid)
		SELECT $1, a.id, $3, $4, $5, NULLIF($6, ''), NULLIF($7, '') FROM (SELECT $2::text AS id) x LEFT JOIN accounts a ON a.id::text = x.id
		ON CONFLICT (session_id) DO NOTHING`, p.SessionID, p.AccountID, p.Plan.Key, p.Plan.Months, p.Cents, p.Email, p.HWID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

type Payment struct {
	SessionID string    `json:"session_id"`
	Plan      string    `json:"plan"`
	Months    int       `json:"months"`
	Cents     int       `json:"amount_cents"`
	At        time.Time `json:"at"`
}

// Payments lists an account's online purchases.
func (s *Service) Payments(ctx context.Context, accountID string) ([]Payment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT session_id, plan, months, amount_cents, created_at FROM billing_payments
		WHERE account_id::text = $1 ORDER BY created_at DESC LIMIT 50`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payment{}
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.SessionID, &p.Plan, &p.Months, &p.Cents, &p.At); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
