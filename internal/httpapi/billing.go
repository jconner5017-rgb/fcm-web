package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/billing"
)

// GET /v1/billing: the plans for sale and the account's online purchases.
func (s *Server) handleBilling(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	pays := []billing.Payment{}
	if s.billing != nil && s.billing.DB != nil {
		var err error
		if pays, err = s.billing.Payments(r.Context(), sess.Account.ID); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": s.billing.Configured(), "plans": billing.Plans, "payments": pays})
}

// POST /v1/billing/checkout {plan}: a Stripe Checkout page to pay on.
func (s *Server) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan string `json:"plan"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, ok := billing.PlanByKey(in.Plan); !ok {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Pick a plan.", nil)
		return
	}
	sess, _ := currentSession(r)
	// A bundle includes the desktop license: send the linked PCs' Hardware IDs
	// along, the way the app's Register window does.
	var hwid string
	if strings.HasPrefix(in.Plan, "bundle") {
		s.db.QueryRowContext(r.Context(), `SELECT coalesce(string_agg(DISTINCT edition || ':' || hwid, ', '), '') FROM devices
			WHERE account_id = $1 AND revoked_at IS NULL`, sess.Account.ID).Scan(&hwid)
	}
	u, err := s.billing.CheckoutURL(sess.Account.ID, in.Plan, hwid)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": u})
}

// POST /v1/billing/stripe-webhook: Stripe tells us a checkout was paid.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.billing.Configured() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := billing.VerifySignature(body, r.Header.Get("Stripe-Signature"), s.billing.WebhookSecret, s.now()); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p, ok, err := billing.ParseEvent(body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusOK) // not ours (e.g. a license sold through checkout.php)
		return
	}
	fresh, err := s.billing.Record(r.Context(), p)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !fresh {
		w.WriteHeader(http.StatusOK) // already handled
		return
	}
	note := fmt.Sprintf("Paid online $%.2f (Stripe %s)", float64(p.Cents)/100, p.SessionID)
	until, err := s.auth.ExtendWebAddon(r.Context(), p.AccountID, p.Plan.Months, p.Plan.Plan, note)
	if err != nil {
		// Let Stripe retry: forget the payment so the retry adds the months.
		s.db.ExecContext(r.Context(), `DELETE FROM billing_payments WHERE session_id = $1`, p.SessionID)
		if errors.Is(err, auth.ErrNotFound) {
			s.log.Error("paid checkout for a missing account", "session", p.SessionID, "account", p.AccountID)
		}
		s.internalError(w, r, err)
		return
	}
	s.office.Audit(r.Context(), auth.Account{Email: "Stripe (online payment)"}, "plan_added", p.AccountID,
		fmt.Sprintf("%d months %s, until %s. %s", p.Plan.Months, p.Plan.Plan, until.Format("2006-01-02"), note))
	s.log.Info("plan paid online", "account", p.AccountID, "plan", p.Plan.Key, "until", until)

	if a, err := s.auth.ByID(r.Context(), p.AccountID); err == nil && s.mail.Enabled() {
		body := fmt.Sprintf("Thanks for buying %s.\n\nFireCardManager Web now runs until %s. Sync, editing and listing are on.\n\n%s/account",
			p.Plan.Name, until.Format("January 2, 2006"), s.webURL)
		if p.Plan.Plan == "bundle" {
			body += "\n\nYour FireCardManager desktop license key will be emailed to you shortly."
		}
		s.mail.Queue(r.Context(), s.db, a.ID, a.Email, "Your FireCardManager Web plan", body)
	}
	w.WriteHeader(http.StatusOK)
}
