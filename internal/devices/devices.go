// Package devices links desktop apps to FCM Web accounts and checks the
// device tokens they sync with.
//
// Linking works like signing a TV app in to a streaming service:
//  1. The desktop calls Start with its PC name, edition, hardware ID and
//     license key. It gets back a short code (KQ7M-2XHD) to show, a link
//     to open in the browser, and a private poll secret.
//  2. The seller, signed in on the website, approves that code.
//  3. The desktop polls Finish with its poll secret and, once approved,
//     receives its device token, exactly once.
package devices

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/license"
)

var (
	ErrBadLicense   = errors.New("that license key isn't valid for this PC")
	ErrExpired      = errors.New("this license has expired; renew it to link this PC")
	ErrCodeNotFound = errors.New("that code wasn't found or has expired; start again from the desktop app")
	ErrCodeUsed     = errors.New("that code was already used")
	ErrPending      = errors.New("waiting for approval on the website")
	ErrNoDevice     = errors.New("this PC isn't linked")
	ErrBadInput     = errors.New("bad input")
)

const (
	LinkTTL   = 10 * time.Minute
	codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
)

type Service struct {
	DB  *sql.DB
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// StartInput is what the desktop sends to begin linking.
type StartInput struct {
	DeviceName string `json:"device_name"`
	Edition    string `json:"edition"` // "sports" or "tcg"
	HWID       string `json:"hwid"`
	LicenseKey string `json:"license_key"` // empty for a desktop still on its trial
	AppVersion string `json:"app_version"`
}

// StartResult goes back to the desktop.
type StartResult struct {
	LinkID     string    `json:"link_id"`
	UserCode   string    `json:"user_code"`
	PollSecret string    `json:"poll_secret"`
	ExpiresAt  time.Time `json:"expires_at"`
	Interval   int       `json:"interval"` // seconds between polls
}

func clean(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func userCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeChars[int(c)%len(codeChars)])
	}
	return string(out), nil
}

// Start checks the license and opens a link request.
func (s *Service) Start(ctx context.Context, in StartInput) (StartResult, error) {
	in.DeviceName = clean(in.DeviceName, 80)
	in.HWID = clean(in.HWID, 120)
	in.AppVersion = clean(in.AppVersion, 40)
	if in.DeviceName == "" {
		in.DeviceName = "My PC"
	}
	if (in.Edition != "sports" && in.Edition != "tcg") || in.HWID == "" || len(in.LicenseKey) > 400 {
		return StartResult{}, ErrBadInput
	}
	state := "trial"
	var expires *time.Time
	if strings.TrimSpace(in.LicenseKey) != "" {
		r := license.Check(in.Edition, in.HWID, in.LicenseKey, s.now())
		switch {
		case r.State == license.Invalid:
			return StartResult{}, ErrBadLicense
		case r.State == license.Expired:
			return StartResult{}, ErrExpired
		}
		state, expires = string(r.State), r.ExpiresAt
	}
	secret, secretHash, err := auth.NewToken()
	if err != nil {
		return StartResult{}, err
	}
	res := StartResult{PollSecret: secret, ExpiresAt: s.now().Add(LinkTTL), Interval: 3}
	for try := 0; ; try++ {
		code, err := userCode()
		if err != nil {
			return StartResult{}, err
		}
		err = s.DB.QueryRowContext(ctx, `
			INSERT INTO device_links (user_code, poll_secret_hash, device_name, edition, hwid, license_key,
				license_expires_at, license_state, app_version, expires_at)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, NULLIF($9, ''), $10)
			ON CONFLICT (user_code) DO NOTHING
			RETURNING id`, code, secretHash, in.DeviceName, in.Edition, in.HWID, strings.TrimSpace(in.LicenseKey),
			expires, state, in.AppVersion, res.ExpiresAt).Scan(&res.LinkID)
		if errors.Is(err, sql.ErrNoRows) && try < 5 {
			continue // that code is taken (very rare): pick another
		}
		if err != nil {
			return StartResult{}, err
		}
		res.UserCode = code
		return res, nil
	}
}

// PendingLink is what the website shows before the seller approves.
type PendingLink struct {
	UserCode   string     `json:"user_code"`
	DeviceName string     `json:"device_name"`
	Edition    string     `json:"edition"`
	License    string     `json:"license_state"`
	LicenseExp *time.Time `json:"license_expires_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

func normCode(c string) string {
	c = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(c)))
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

// Lookup shows a code's request to the signed-in seller.
func (s *Service) Lookup(ctx context.Context, code string) (PendingLink, error) {
	var p PendingLink
	var exp sql.NullTime
	var approved sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT user_code, device_name, edition, license_state, license_expires_at, expires_at, approved_at
		FROM device_links WHERE user_code = $1 AND expires_at > $2 AND finished_at IS NULL`, normCode(code), s.now()).
		Scan(&p.UserCode, &p.DeviceName, &p.Edition, &p.License, &exp, &p.ExpiresAt, &approved)
	if errors.Is(err, sql.ErrNoRows) {
		return PendingLink{}, ErrCodeNotFound
	}
	if err != nil {
		return PendingLink{}, err
	}
	if approved.Valid {
		return PendingLink{}, ErrCodeUsed
	}
	if exp.Valid {
		t := exp.Time
		p.LicenseExp = &t
	}
	return p, nil
}

// Approve ties a pending code to the signed-in account.
func (s *Service) Approve(ctx context.Context, accountID, code string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE device_links SET approved_by = $2, approved_at = $3
		WHERE user_code = $1 AND expires_at > $3 AND approved_at IS NULL AND finished_at IS NULL`,
		normCode(code), accountID, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, lerr := s.Lookup(ctx, code); errors.Is(lerr, ErrCodeUsed) {
			return ErrCodeUsed
		}
		return ErrCodeNotFound
	}
	return nil
}

// FinishResult is the desktop's device token, handed out once.
type FinishResult struct {
	DeviceID     string `json:"device_id"`
	DeviceToken  string `json:"device_token"`
	AccountEmail string `json:"account_email"`
}

// Finish is polled by the desktop. Before approval it returns ErrPending.
func (s *Service) Finish(ctx context.Context, linkID, pollSecret string) (FinishResult, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return FinishResult{}, err
	}
	defer tx.Rollback()
	var secretHash []byte
	var name, edition, hwid, state string
	var key, appVersion sql.NullString
	var licExp sql.NullTime
	var expires time.Time
	var approvedBy sql.NullString
	var finished sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT poll_secret_hash, device_name, edition, hwid, license_key, license_state,
			license_expires_at, app_version, expires_at, approved_by, finished_at
		FROM device_links WHERE id::text = $1 FOR UPDATE`, linkID).
		Scan(&secretHash, &name, &edition, &hwid, &key, &state, &licExp, &appVersion, &expires, &approvedBy, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return FinishResult{}, ErrCodeNotFound
	}
	if err != nil {
		return FinishResult{}, err
	}
	if subtle.ConstantTimeCompare(secretHash, auth.HashToken(pollSecret)) != 1 {
		return FinishResult{}, ErrCodeNotFound
	}
	if finished.Valid {
		return FinishResult{}, ErrCodeUsed
	}
	if !approvedBy.Valid {
		if s.now().After(expires) {
			return FinishResult{}, ErrCodeNotFound
		}
		return FinishResult{}, ErrPending
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		return FinishResult{}, err
	}
	// Re-linking the same app on the same PC replaces the old link.
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at = $4 WHERE account_id = $1 AND edition = $2 AND hwid = $3 AND revoked_at IS NULL`,
		approvedBy.String, edition, hwid, s.now()); err != nil {
		return FinishResult{}, err
	}
	res := FinishResult{DeviceToken: token}
	if err := tx.QueryRowContext(ctx, `INSERT INTO devices (account_id, name, edition, hwid, token_hash, app_version, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, approvedBy.String, name, edition, hwid, tokenHash, appVersion, s.now()).
		Scan(&res.DeviceID); err != nil {
		return FinishResult{}, err
	}
	if key.Valid {
		if _, err := tx.ExecContext(ctx, `INSERT INTO licenses (account_id, edition, hwid, key_hash, expires_at, verified_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (account_id, edition, hwid) DO UPDATE SET key_hash = EXCLUDED.key_hash,
				expires_at = EXCLUDED.expires_at, verified_at = EXCLUDED.verified_at`,
			approvedBy.String, edition, hwid, auth.HashToken(strings.TrimSpace(key.String)), licExp, s.now()); err != nil {
			return FinishResult{}, err
		}
	}
	// The key isn't needed any more: don't keep it.
	if _, err := tx.ExecContext(ctx, `UPDATE device_links SET finished_at = $2, license_key = NULL WHERE id = $1::uuid`, linkID, s.now()); err != nil {
		return FinishResult{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT email FROM accounts WHERE id = $1`, approvedBy.String).Scan(&res.AccountEmail); err != nil {
		return FinishResult{}, err
	}
	return res, tx.Commit()
}

// Device is a linked desktop app.
type Device struct {
	ID         string     `json:"id"`
	AccountID  string     `json:"-"`
	Name       string     `json:"name"`
	Edition    string     `json:"edition"`
	AppVersion string     `json:"app_version,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// ByToken checks a device token and records that the device was seen.
func (s *Service) ByToken(ctx context.Context, token string) (Device, error) {
	if token == "" {
		return Device{}, ErrNoDevice
	}
	var d Device
	var ver sql.NullString
	var seen sql.NullTime
	err := s.DB.QueryRowContext(ctx, `UPDATE devices SET last_seen_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING id, account_id, name, edition, app_version, created_at, last_seen_at`, auth.HashToken(token), s.now()).
		Scan(&d.ID, &d.AccountID, &d.Name, &d.Edition, &ver, &d.CreatedAt, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNoDevice
	}
	if err != nil {
		return Device{}, err
	}
	d.AppVersion = ver.String
	if seen.Valid {
		t := seen.Time
		d.LastSeenAt = &t
	}
	return d, nil
}

// List returns an account's linked devices.
func (s *Service) List(ctx context.Context, accountID string) ([]Device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, edition, coalesce(app_version, ''), created_at, last_seen_at
		FROM devices WHERE account_id = $1 AND revoked_at IS NULL ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		var seen sql.NullTime
		if err := rows.Scan(&d.ID, &d.Name, &d.Edition, &d.AppVersion, &d.CreatedAt, &seen); err != nil {
			return nil, err
		}
		if seen.Valid {
			t := seen.Time
			d.LastSeenAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Revoke unlinks a device. Its token stops working at once.
func (s *Service) Revoke(ctx context.Context, accountID, deviceID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE devices SET revoked_at = $3 WHERE id::text = $2 AND account_id = $1 AND revoked_at IS NULL`,
		accountID, deviceID, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoDevice
	}
	return nil
}

// Editions returns the editions an account has linked a licensed (or
// trial) desktop for, so the website shows only those.
func (s *Service) Editions(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT edition FROM devices WHERE account_id = $1 AND revoked_at IS NULL ORDER BY edition`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
