package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrMFANotConfigured = errors.New("authenticator codes aren't set up on this server (MFA_ENCRYPTION_KEY)")
	ErrMFAEnrolled      = errors.New("an authenticator is already set up for this account")
	ErrMFANotEnrolled   = errors.New("set up an authenticator first")
	ErrBadCode          = errors.New("that code didn't work; check your authenticator app and try again")
	ErrMFALocked        = errors.New("too many wrong codes; try again in 15 minutes")
)

const (
	MaxCodeFailures = 5
	LockFor         = 15 * time.Minute
	backupCodeCount = 10
	issuer          = "FireCardManager"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// MFA stores and checks account authenticator (TOTP, RFC 6238) secrets.
// Copied from the Slabs & Singles staff sign-in check.
type MFA struct {
	DB  *sql.DB
	Key []byte // 32-byte AES key from MFA_ENCRYPTION_KEY; nil disables 2FA set-up
	Now func() time.Time
}

func (m *MFA) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *MFA) aead() (cipher.AEAD, error) {
	if len(m.Key) != 32 {
		return nil, ErrMFANotConfigured
	}
	block, err := aes.NewCipher(m.Key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (m *MFA) seal(secret []byte) ([]byte, error) {
	g, err := m.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	return g.Seal(nonce, nonce, secret, nil), nil
}

func (m *MFA) open(box []byte) ([]byte, error) {
	g, err := m.aead()
	if err != nil {
		return nil, err
	}
	if len(box) < g.NonceSize() {
		return nil, errors.New("bad secret")
	}
	return g.Open(nil, box[:g.NonceSize()], box[g.NonceSize():], nil)
}

// Status is "setup_required", "locked" or "ready".
func (m *MFA) Status(ctx context.Context, accountID string) (string, error) {
	var confirmed sql.NullTime
	var locked sql.NullTime
	err := m.DB.QueryRowContext(ctx, `SELECT confirmed_at, locked_until FROM account_mfa WHERE account_id = $1`, accountID).
		Scan(&confirmed, &locked)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !confirmed.Valid) {
		return "setup_required", nil
	}
	if err != nil {
		return "", err
	}
	if locked.Valid && locked.Time.After(m.now()) {
		return "locked", nil
	}
	return "ready", nil
}

// Setup starts (or restarts) enrollment and returns the secret and the
// otpauth:// link the authenticator app scans.
func (m *MFA) Setup(ctx context.Context, accountID, email string) (secret, link string, err error) {
	if _, err := m.aead(); err != nil {
		return "", "", err
	}
	var confirmed bool
	err = m.DB.QueryRowContext(ctx, `SELECT confirmed_at IS NOT NULL FROM account_mfa WHERE account_id = $1`, accountID).Scan(&confirmed)
	if err == nil && confirmed {
		return "", "", ErrMFAEnrolled
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	raw := make([]byte, 20)
	rand.Read(raw)
	box, err := m.seal(raw)
	if err != nil {
		return "", "", err
	}
	if _, err := m.DB.ExecContext(ctx, `
		INSERT INTO account_mfa (account_id, secret_enc) VALUES ($1, $2)
		ON CONFLICT (account_id) DO UPDATE SET secret_enc = EXCLUDED.secret_enc, confirmed_at = NULL,
			backup_hashes = '{}', failed_count = 0, locked_until = NULL, last_step = 0, updated_at = now()`,
		accountID, box); err != nil {
		return "", "", err
	}
	secret = b32.EncodeToString(raw)
	v := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	link = "otpauth://totp/" + url.PathEscape(issuer+":"+email) + "?" + v.Encode()
	return secret, link, nil
}

// TOTP is the 6-digit code for a secret at a time step.
func TOTP(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	h := hmac.New(sha1.New, secret)
	h.Write(msg[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1_000_000)
}

func step(t time.Time) int64 { return t.Unix() / 30 }

func normalizeCode(code string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
}

func hashBackup(code string) string {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}

// Confirm finishes enrollment with a first code and returns one-time
// backup codes (shown once; only hashes are kept).
func (m *MFA) Confirm(ctx context.Context, accountID, code string) ([]string, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var box []byte
	var confirmed bool
	err = tx.QueryRowContext(ctx, `SELECT secret_enc, confirmed_at IS NOT NULL FROM account_mfa WHERE account_id = $1 FOR UPDATE`,
		accountID).Scan(&box, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMFANotEnrolled
	}
	if err != nil {
		return nil, err
	}
	if confirmed {
		return nil, ErrMFAEnrolled
	}
	secret, err := m.open(box)
	if err != nil {
		return nil, err
	}
	st, ok := m.match(secret, code, 0)
	if !ok {
		return nil, ErrBadCode
	}
	codes := make([]string, backupCodeCount)
	hashes := make([]string, backupCodeCount)
	for i := range codes {
		b := make([]byte, 5)
		rand.Read(b)
		c := strings.ToLower(b32.EncodeToString(b)) // 8 characters
		codes[i] = c[:4] + "-" + c[4:]
		hashes[i] = hashBackup(codes[i])
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_mfa SET confirmed_at = now(), backup_hashes = $2, last_step = $3,
		failed_count = 0, updated_at = now() WHERE account_id = $1`, accountID, pq.Array(hashes), st); err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// match checks a code against the current time step and one either side,
// never accepting a step at or before lastStep (so a code works once).
func (m *MFA) match(secret []byte, code string, lastStep int64) (int64, bool) {
	code = normalizeCode(code)
	if len(code) != 6 {
		return 0, false
	}
	now := step(m.now())
	for _, s := range []int64{now - 1, now, now + 1} {
		if s > lastStep && hmac.Equal([]byte(TOTP(secret, s)), []byte(code)) {
			return s, true
		}
	}
	return 0, false
}

// VerifyResult says how a sign-in check went.
type VerifyResult struct {
	UsedBackup  bool
	BackupsLeft int
	JustLocked  bool // this failure locked the account
}

// Verify checks an authenticator or backup code. Five wrong codes in a
// row lock the check for 15 minutes.
func (m *MFA) Verify(ctx context.Context, accountID, code string) (VerifyResult, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return VerifyResult{}, err
	}
	defer tx.Rollback()
	var box []byte
	var confirmed, locked sql.NullTime
	var backups []string
	var fails int
	var last int64
	err = tx.QueryRowContext(ctx, `SELECT secret_enc, confirmed_at, locked_until, backup_hashes, failed_count, last_step
		FROM account_mfa WHERE account_id = $1 FOR UPDATE`, accountID).Scan(&box, &confirmed, &locked, pq.Array(&backups), &fails, &last)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !confirmed.Valid) {
		return VerifyResult{}, ErrMFANotEnrolled
	}
	if err != nil {
		return VerifyResult{}, err
	}
	if locked.Valid && locked.Time.After(m.now()) {
		return VerifyResult{}, ErrMFALocked
	}
	secret, err := m.open(box)
	if err != nil {
		return VerifyResult{}, err
	}
	if st, ok := m.match(secret, code, last); ok {
		_, err := tx.ExecContext(ctx, `UPDATE account_mfa SET last_step = $2, failed_count = 0, locked_until = NULL,
			updated_at = now() WHERE account_id = $1`, accountID, st)
		if err != nil {
			return VerifyResult{}, err
		}
		return VerifyResult{BackupsLeft: len(backups)}, tx.Commit()
	}
	h := hashBackup(code)
	for i, b := range backups {
		if hmac.Equal([]byte(b), []byte(h)) {
			rest := append(append([]string{}, backups[:i]...), backups[i+1:]...)
			if _, err := tx.ExecContext(ctx, `UPDATE account_mfa SET backup_hashes = $2, failed_count = 0, locked_until = NULL,
				updated_at = now() WHERE account_id = $1`, accountID, pq.Array(rest)); err != nil {
				return VerifyResult{}, err
			}
			return VerifyResult{UsedBackup: true, BackupsLeft: len(rest)}, tx.Commit()
		}
	}
	fails++
	res := VerifyResult{}
	var lockUntil *time.Time
	if fails >= MaxCodeFailures {
		t := m.now().Add(LockFor)
		lockUntil, fails, res.JustLocked = &t, 0, true
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_mfa SET failed_count = $2, locked_until = $3, updated_at = now()
		WHERE account_id = $1`, accountID, fails, lockUntil); err != nil {
		return VerifyResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return VerifyResult{}, err
	}
	if res.JustLocked {
		return res, ErrMFALocked
	}
	return res, ErrBadCode
}

// Reset removes an account's authenticator (turning 2FA off).
func (m *MFA) Reset(ctx context.Context, accountID string) error {
	_, err := m.DB.ExecContext(ctx, `DELETE FROM account_mfa WHERE account_id = $1`, accountID)
	return err
}

// ParseKey reads MFA_ENCRYPTION_KEY: 64 hex characters, base64 of 32 bytes,
// or any random string of 32+ characters.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []interface{ DecodeString(string) ([]byte, error) }{base64.StdEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	// Any other long random value (Render's "Generate" button makes one) is
	// stretched to a 32-byte key with SHA-256.
	if len(s) >= 32 {
		sum := sha256.Sum256([]byte(s))
		return sum[:], nil
	}
	return nil, errors.New("MFA_ENCRYPTION_KEY must be at least 32 random characters")
}
