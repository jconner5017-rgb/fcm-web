// Package auth handles FCM Web accounts, passwords, sign-in sessions and the
// optional authenticator (2FA) step. Patterned on Slabs & Singles, trimmed to
// what a seller's inventory account needs.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
)

var (
	ErrEmailTaken         = errors.New("an account with that email already exists")
	ErrInvalidCredentials = errors.New("wrong email or password")
	ErrAccountDisabled    = errors.New("this account is disabled")
	ErrNoSession          = errors.New("not signed in")
	ErrNotFound           = errors.New("account not found")
)

// ValidationError lists problems with submitted fields, keyed by field name.
type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "invalid input" }

// Account is a seller's FCM Web account.
type Account struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	EmailVerified   bool       `json:"email_verified"`
	IsAdmin         bool       `json:"is_admin"`
	Status          string     `json:"status"`
	SyncTrialEndsAt time.Time  `json:"sync_trial_ends_at"`
	WebAddonAt      *time.Time `json:"web_addon_at,omitempty"`    // first time the add-on was bought
	WebAddonUntil   *time.Time `json:"web_addon_until,omitempty"` // the add-on runs until this date
	WebPlan         string     `json:"web_plan,omitempty"`        // "addon" (with an FCM license) or "standalone"
	CreatedAt       time.Time  `json:"created_at"`
	// Why the free trial ended early: "email", "pc" or "inventory" (an earlier account had it).
	TrialEndedReason string `json:"trial_ended_reason,omitempty"`
	// Back office access the owner granted: "support" or "manager" ("" none).
	// Owners (IsAdmin) have all of it.
	StaffRole       string `json:"staff_role,omitempty"`
	SuspendedReason string `json:"suspended_reason,omitempty"`
}

// Back office permissions: view (members, alerts, audit), act (notes,
// suspend, sign out), billing (plans, trials, closing accounts), staff
// (granting access - owners only).
func (a Account) Can(perm string) bool {
	if a.IsAdmin {
		return true
	}
	switch a.StaffRole {
	case "manager":
		return perm == "view" || perm == "act" || perm == "billing"
	case "support":
		return perm == "view" || perm == "act"
	}
	return false
}

// AddonActive: FireCardManager Web is paid up (alone, $39.99 / 6 months or
// $59.99 / 12, or in the bundle with FCM desktop, $59.99 / $89.99).
func (a Account) AddonActive(now time.Time) bool {
	return a.WebAddonUntil != nil && now.Before(*a.WebAddonUntil)
}

// SyncAllowed: during the 30-day trial, or while the web add-on runs.
// Admins always may (for testing).
func (a Account) SyncAllowed(now time.Time) bool {
	return a.IsAdmin || a.AddonActive(now) || now.Before(a.SyncTrialEndsAt)
}

// Session is what a session token proves.
type Session struct {
	Account Account
	MFAOK   bool // false: password checked, authenticator step still to do
}

type Service struct {
	DB          *sql.DB
	SessionTTL  time.Duration
	AdminEmails []string // become admins when they sign up (ADMIN_EMAILS)
	Now         func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) isAdminEmail(email string) bool {
	for _, a := range s.AdminEmails {
		if strings.EqualFold(strings.TrimSpace(a), email) {
			return true
		}
	}
	return false
}

const (
	minPasswordLen = 10
	maxPasswordLen = 128
)

func validateSignup(email, password string) error {
	f := map[string]string{}
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		f["email"] = "Enter a valid email address."
	}
	if n := utf8.RuneCountInString(password); n < minPasswordLen || n > maxPasswordLen {
		f["password"] = "Use 10 to 128 characters."
	}
	if len(f) > 0 {
		return &ValidationError{Fields: f}
	}
	return nil
}

const accountCols = `a.id, a.email, a.email_verified_at IS NOT NULL, a.is_admin, a.status, a.sync_trial_ends_at, a.web_addon_at, a.web_addon_until, coalesce(a.web_plan, ''), a.created_at, coalesce(a.trial_ended_reason, ''), coalesce(a.staff_role, ''), coalesce(a.suspended_reason, '')`

func scanAccount(row interface{ Scan(...any) error }, extra ...any) (Account, error) {
	var a Account
	var addon, until sql.NullTime
	dst := append([]any{&a.ID, &a.Email, &a.EmailVerified, &a.IsAdmin, &a.Status, &a.SyncTrialEndsAt, &addon, &until, &a.WebPlan, &a.CreatedAt, &a.TrialEndedReason, &a.StaffRole, &a.SuspendedReason}, extra...)
	err := row.Scan(dst...)
	if addon.Valid {
		t := addon.Time
		a.WebAddonAt = &t
	}
	if until.Valid {
		t := until.Time
		a.WebAddonUntil = &t
	}
	return a, err
}

// Signup creates an account. The 30-day sync trial starts now.
func (s *Service) Signup(ctx context.Context, email, password string) (Account, error) {
	email = strings.TrimSpace(email)
	if err := validateSignup(email, password); err != nil {
		return Account{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Account{}, err
	}
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `
		INSERT INTO accounts AS a (email, password_hash, is_admin, sync_trial_ends_at)
		VALUES ($1, $2, $3, $4)
		RETURNING `+accountCols, email, hash, s.isAdminEmail(email), s.now().Add(30*24*time.Hour)))
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return Account{}, ErrEmailTaken
		}
		return Account{}, err
	}
	return a, nil
}

// dummyHash is checked when an email isn't found, so a failed sign-in takes
// the same time whether or not the account exists.
var dummyHash, _ = HashPassword("not-a-real-password")

func (s *Service) loginEvent(ctx context.Context, accountID *string, email, ip, ua string, ok bool, reason string) {
	if len(ua) > 300 {
		ua = ua[:300]
	}
	s.DB.ExecContext(ctx, `INSERT INTO login_events (account_id, email, ip, user_agent, success, reason)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`, accountID, email, ip, ua, ok, reason)
}

// Login checks the password and starts a session. mfaNeeded means the
// account has 2FA on: the session can only finish the authenticator step
// until VerifySessionMFA marks it.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (a Account, token string, mfaNeeded bool, err error) {
	email = strings.TrimSpace(email)
	var hash string
	a, err = scanAccount(s.DB.QueryRowContext(ctx, `
		SELECT `+accountCols+`, a.password_hash,
			EXISTS (SELECT 1 FROM account_mfa m WHERE m.account_id = a.id AND m.confirmed_at IS NOT NULL)
		FROM accounts a WHERE lower(a.email) = lower($1)`, email), &hash, &mfaNeeded)
	if errors.Is(err, sql.ErrNoRows) {
		VerifyPassword(password, dummyHash)
		s.loginEvent(ctx, nil, email, ip, userAgent, false, "unknown_email")
		return Account{}, "", false, ErrInvalidCredentials
	}
	if err != nil {
		return Account{}, "", false, err
	}
	if !VerifyPassword(password, hash) {
		s.loginEvent(ctx, &a.ID, email, ip, userAgent, false, "bad_password")
		return Account{}, "", false, ErrInvalidCredentials
	}
	if a.Status != "active" {
		s.loginEvent(ctx, &a.ID, email, ip, userAgent, false, "disabled")
		return Account{}, "", false, ErrAccountDisabled
	}
	token, err = s.createSession(ctx, a.ID, !mfaNeeded, ip, userAgent)
	if err != nil {
		return Account{}, "", false, err
	}
	s.loginEvent(ctx, &a.ID, email, ip, userAgent, true, map[bool]string{true: "mfa_pending", false: ""}[mfaNeeded])
	return a, token, mfaNeeded, nil
}

// StartSession signs an account in directly (right after sign-up).
func (s *Service) StartSession(ctx context.Context, accountID, ip, userAgent string) (string, error) {
	return s.createSession(ctx, accountID, true, ip, userAgent)
}

func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	t := base64.RawURLEncoding.EncodeToString(raw)
	return t, HashToken(t), nil
}

// NewToken returns a random URL-safe token and its hash (for device tokens).
func NewToken() (token string, hash []byte, err error) { return newToken() }

// HashToken is how every bearer token is stored: never the token itself.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (s *Service) createSession(ctx context.Context, accountID string, mfaOK bool, ip, userAgent string) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	ttl := s.SessionTTL
	if !mfaOK {
		ttl = 10 * time.Minute // just long enough to type the code
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, account_id, mfa_ok, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`, hash, accountID, mfaOK, s.now().Add(ttl), ip, userAgent)
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionFor returns the account behind a session token.
func (s *Service) SessionFor(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNoSession
	}
	var sess Session
	var err error
	sess.Account, err = scanAccount(s.DB.QueryRowContext(ctx, `
		SELECT `+accountCols+`, s.mfa_ok
		FROM sessions s JOIN accounts a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND a.status = 'active'`,
		HashToken(token), s.now()), &sess.MFAOK)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	return sess, err
}

// MarkSessionMFA finishes the authenticator step: the session becomes a
// full one for the normal session length.
func (s *Service) MarkSessionMFA(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET mfa_ok = true, expires_at = $2 WHERE token_hash = $1`,
		HashToken(token), s.now().Add(s.SessionTTL))
	return err
}

// Logout ends one session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, HashToken(token))
	return err
}

// LogoutOthers ends every other session of an account (after a password change).
func (s *Service) LogoutOthers(ctx context.Context, accountID, keepToken string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE account_id = $1 AND token_hash <> $2`,
		accountID, HashToken(keepToken))
	return err
}

// PurgeExpiredSessions removes expired rows; run periodically.
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, s.now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ByID returns an account.
func (s *Service) ByID(ctx context.Context, id string) (Account, error) {
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts a WHERE a.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// ByEmail returns an account by email (any case).
func (s *Service) ByEmail(ctx context.Context, email string) (Account, error) {
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts a WHERE lower(a.email) = lower($1)`,
		strings.TrimSpace(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// ChangePassword checks the current password and sets a new one.
func (s *Service) ChangePassword(ctx context.Context, accountID, current, next string) error {
	var hash string
	if err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id = $1`, accountID).Scan(&hash); err != nil {
		return err
	}
	if !VerifyPassword(current, hash) {
		return &ValidationError{Fields: map[string]string{"current_password": "That isn't your current password."}}
	}
	return s.SetPassword(ctx, s.DB, accountID, next)
}

// Close switches the account off now and queues it for deletion (photos,
// inventory, keys, eBay connection, everything) by PurgeClosed. With
// checkPassword, password must be the account's password.
func (s *Service) Close(ctx context.Context, accountID, password string, checkPassword bool) error {
	if checkPassword {
		var hash string
		if err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id = $1`, accountID).Scan(&hash); err != nil {
			return err
		}
		if !VerifyPassword(password, hash) {
			return &ValidationError{Fields: map[string]string{"password": "That isn't your password."}}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET status = 'disabled', closing_at = now(), updated_at = now() WHERE id::text = $1`, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Signed out everywhere, PCs unlinked, saved keys and eBay forgotten right away.
	for _, q := range []string{`DELETE FROM sessions WHERE account_id::text = $1`, `UPDATE devices SET revoked_at = now() WHERE account_id::text = $1 AND revoked_at IS NULL`,
		`DELETE FROM account_ai_keys WHERE account_id::text = $1`, `DELETE FROM account_market_keys WHERE account_id::text = $1`,
		`DELETE FROM account_ebay WHERE account_id::text = $1`} {
		if _, err := tx.ExecContext(ctx, q, accountID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Closing lists accounts waiting to be deleted.
func (s *Service) Closing(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM accounts WHERE closing_at IS NOT NULL ORDER BY closing_at LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Remove deletes a closing account and everything in it (call after its photos are gone).
func (s *Service) Remove(ctx context.Context, accountID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1 AND closing_at IS NOT NULL`, accountID)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// SetPassword sets a new password (also used by the reset link).
func (s *Service) SetPassword(ctx context.Context, q execer, accountID, password string) error {
	if n := utf8.RuneCountInString(password); n < minPasswordLen || n > maxPasswordLen {
		return &ValidationError{Fields: map[string]string{"password": "Use 10 to 128 characters."}}
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE accounts SET password_hash = $2, updated_at = now() WHERE id = $1`, accountID, hash)
	return err
}

// MarkVerified records that the account's email was confirmed.
func (s *Service) MarkVerified(ctx context.Context, q execer, accountID, email string) error {
	_, err := q.ExecContext(ctx, `UPDATE accounts SET email_verified_at = coalesce(email_verified_at, now()), updated_at = now()
		WHERE id = $1 AND lower(email) = lower($2)`, accountID, email)
	return err
}

// ---- Admin ----

// AdminRow is one account in the admin list.
type AdminRow struct {
	Account
	WebAddonNote string     `json:"web_addon_note,omitempty"`
	Devices      int        `json:"devices"`
	Cards        int        `json:"cards"`
	LastSync     *time.Time `json:"last_sync,omitempty"`
}

// ListAccounts returns accounts newest first, optionally filtered by email
// (or "id:<uuid>" for one account) and by a back-office filter (a SQL
// condition on accounts a, from backoffice.Filters).
func (s *Service) ListAccounts(ctx context.Context, q string, where string, limit int) ([]AdminRow, error) {
	q = strings.TrimSpace(q)
	cond := `($1 = '' OR a.email ILIKE '%' || $1 || '%' OR a.signup_ip = $1)`
	if strings.HasPrefix(where, "id:") {
		cond, q = `a.id::text = $1`, strings.ToLower(strings.TrimPrefix(where, "id:"))
	} else if where != "" {
		cond += " AND " + where
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+accountCols+`, coalesce(a.web_addon_note, ''),
			(SELECT count(*) FROM devices d WHERE d.account_id = a.id AND d.revoked_at IS NULL),
			(SELECT count(*) FROM sync_cards c WHERE c.account_id = a.id AND c.deleted_at IS NULL),
			(SELECT max(d.last_seen_at) FROM devices d WHERE d.account_id = a.id)
		FROM accounts a
		WHERE `+cond+`
		ORDER BY a.created_at DESC LIMIT $2`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminRow{}
	for rows.Next() {
		var r AdminRow
		var last sql.NullTime
		r.Account, err = scanAccount(rows, &r.WebAddonNote, &r.Devices, &r.Cards, &last)
		if err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			r.LastSync = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddonMonths are the terms sold: 6 and 12 months, matching the FCM license
// lengths. WebPlans: "web" (FCM Web alone, $39.99 / $59.99) or "bundle"
// (FCM desktop + FCM Web, $59.99 / $89.99). "addon" and "standalone" were
// earlier plans; accounts on them keep running until their date.
var AddonMonths = map[int]bool{6: true, 12: true}

var WebPlans = map[string]bool{"web": true, "bundle": true, "addon": true, "standalone": true}

// ExtendWebAddon adds months to an account's web add-on, counted from its
// current end date if it's still running (a renewal loses no days),
// otherwise from now. note records the order (number, amount).
func (s *Service) ExtendWebAddon(ctx context.Context, accountID string, months int, plan, note string) (time.Time, error) {
	if !AddonMonths[months] {
		return time.Time{}, &ValidationError{Fields: map[string]string{"months": "Use 6 or 12 months."}}
	}
	if plan == "" {
		plan = "web"
	}
	if !WebPlans[plan] {
		return time.Time{}, &ValidationError{Fields: map[string]string{"plan": "Use web or bundle."}}
	}
	note = strings.TrimSpace(note)
	if len(note) > 300 {
		note = note[:300]
	}
	var until time.Time
	err := s.DB.QueryRowContext(ctx, `UPDATE accounts SET
			web_addon_at = coalesce(web_addon_at, $4),
			web_addon_until = (CASE WHEN web_addon_until > $4 THEN web_addon_until ELSE $4 END) + make_interval(months => $2),
			web_addon_note = coalesce(NULLIF($3, ''), web_addon_note), web_plan = $5, updated_at = now()
		WHERE id::text = $1 RETURNING web_addon_until`, accountID, months, note, s.now(), plan).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	return until, err
}

// EndWebAddon stops the add-on now (a refund or a mistake).
func (s *Service) EndWebAddon(ctx context.Context, accountID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET web_addon_until = NULL, updated_at = now() WHERE id::text = $1`, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExtendTrial pushes an account's sync trial to a date (for a customer who
// needs a few more days).
func (s *Service) ExtendTrial(ctx context.Context, accountID string, until time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_trial_ends_at = $2,
		trial_ended_reason = CASE WHEN $2 > now() THEN 'allowed' ELSE trial_ended_reason END, updated_at = now() WHERE id = $1`, accountID, until)
	return err
}

// PromoteAdmins makes every registered ADMIN_EMAILS account an admin (run at
// start-up, so adding an email to the setting is enough).
func (s *Service) PromoteAdmins(ctx context.Context) error {
	for _, a := range s.AdminEmails {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE accounts SET is_admin = true, updated_at = now()
			WHERE lower(email) = lower($1) AND NOT is_admin`, a); err != nil {
			return err
		}
	}
	return nil
}
