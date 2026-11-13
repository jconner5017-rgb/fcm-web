// Package ebay connects a seller's eBay account to FireCardManager Web
// (OAuth authorization code grant) and keeps a working access token for the
// listing and order calls. The app id, cert id and RuName are the shared
// FireCardManager eBay application's, set in the server's environment.
package ebay

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Scopes asked for: listing (inventory), business policies and location
// (account), orders (fulfillment) and the seller's user name (identity).
var Scopes = []string{
	"https://api.ebay.com/oauth/api_scope",
	"https://api.ebay.com/oauth/api_scope/sell.inventory",
	"https://api.ebay.com/oauth/api_scope/sell.account",
	"https://api.ebay.com/oauth/api_scope/sell.fulfillment",
	"https://api.ebay.com/oauth/api_scope/commerce.identity.readonly",
}

var (
	ErrNotConfigured = errors.New("eBay isn't set up on this server yet")
	ErrNotConnected  = errors.New("eBay isn't connected")
	ErrBadState      = errors.New("that eBay sign-in link expired or was already used")
	ErrReconnect     = errors.New("eBay ended this connection; connect eBay again")
)

// Sealer encrypts saved tokens (auth.MFA).
type Sealer interface {
	SealFor(purpose string, plain []byte) ([]byte, error)
	OpenFor(purpose string, box []byte) ([]byte, error)
}

const purpose = "ebay-token"

type Service struct {
	DB     *sql.DB
	Sealer Sealer
	AppID  string
	CertID string
	RuName string
	// Where eBay lives (tests point these at a fake).
	AuthURL string // https://auth.ebay.com
	APIURL  string // https://api.ebay.com
	IDURL   string // https://apiz.ebay.com
	HTTP    *http.Client
	Now     func() time.Time

	mu sync.Mutex // one token refresh at a time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func orDefault(v, d string) string {
	if v != "" {
		return strings.TrimRight(v, "/")
	}
	return d
}

func (s *Service) authBase() string { return orDefault(s.AuthURL, "https://auth.ebay.com") }
func (s *Service) APIBase() string  { return orDefault(s.APIURL, "https://api.ebay.com") }
func (s *Service) idBase() string   { return orDefault(s.IDURL, "https://apiz.ebay.com") }

// Configured reports whether the app id, cert id and RuName are set.
func (s *Service) Configured() bool {
	return s != nil && s.AppID != "" && s.CertID != "" && s.RuName != "" && s.Sealer != nil
}

// StartURL makes the eBay sign-in link for the account.
func (s *Service) StartURL(ctx context.Context, accountID string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM ebay_oauth_states WHERE created_at < now() - interval '1 hour'`); err != nil {
		return "", err
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ebay_oauth_states (state, account_id) VALUES ($1, $2)`, state, accountID); err != nil {
		return "", err
	}
	q := url.Values{
		"client_id":     {s.AppID},
		"redirect_uri":  {s.RuName},
		"response_type": {"code"},
		"scope":         {strings.Join(Scopes, " ")},
		"state":         {state},
	}
	return s.authBase() + "/oauth2/authorize?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

func (s *Service) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	var out tokenResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.APIBase()+"/identity/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s.AppID+":"+s.CertID)))
	res, err := s.client().Do(req)
	if err != nil {
		return out, fmt.Errorf("couldn't reach eBay: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = json.Unmarshal(b, &out)
	if res.StatusCode >= 300 || out.AccessToken == "" {
		if out.Error == "invalid_grant" {
			return out, ErrReconnect
		}
		return out, fmt.Errorf("eBay sign-in failed (%d %s %s)", res.StatusCode, out.Error, out.ErrorDescription)
	}
	return out, nil
}

// Finish trades the code eBay sent back for tokens and saves them.
func (s *Service) Finish(ctx context.Context, accountID, state, code string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	var owner string
	err := s.DB.QueryRowContext(ctx, `DELETE FROM ebay_oauth_states WHERE state = $1 AND created_at > now() - interval '10 minutes' RETURNING account_id`,
		state).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != accountID) {
		return "", ErrBadState
	}
	if err != nil {
		return "", err
	}
	t, err := s.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.RuName}})
	if err != nil {
		return "", err
	}
	user := s.userName(ctx, t.AccessToken)
	refresh, err := s.Sealer.SealFor(purpose, []byte(t.RefreshToken))
	if err != nil {
		return "", err
	}
	access, err := s.Sealer.SealFor(purpose, []byte(t.AccessToken))
	if err != nil {
		return "", err
	}
	now := s.now()
	refreshLife := time.Duration(t.RefreshTokenExpiresIn) * time.Second
	if refreshLife <= 0 {
		refreshLife = 18 * 30 * 24 * time.Hour
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO account_ebay (account_id, refresh_enc, refresh_expires_at, access_enc, access_expires_at, ebay_user, connected_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (account_id) DO UPDATE SET refresh_enc = $2, refresh_expires_at = $3, access_enc = $4, access_expires_at = $5,
			ebay_user = $6, connected_at = $7, updated_at = $7`,
		accountID, refresh, now.Add(refreshLife), access, now.Add(time.Duration(t.ExpiresIn)*time.Second), user, now)
	return user, err
}

// userName is the seller's eBay user id ("" if eBay doesn't say).
func (s *Service) userName(ctx context.Context, access string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.idBase()+"/commerce/identity/v1/user/", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := s.client().Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var u struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&u)
	return u.Username
}

// Status is what the Account page shows.
type Status struct {
	Configured  bool       `json:"configured"`
	Connected   bool       `json:"connected"`
	User        string     `json:"user,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"` // when eBay asks to connect again (about 18 months)
}

func (s *Service) Status(ctx context.Context, accountID string) (Status, error) {
	st := Status{Configured: s.Configured()}
	var at, exp time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT ebay_user, connected_at, refresh_expires_at FROM account_ebay WHERE account_id = $1`, accountID).
		Scan(&st.User, &at, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected, st.ConnectedAt, st.ExpiresAt = exp.After(s.now()), &at, &exp
	return st, nil
}

// Disconnect forgets the tokens (the seller can also revoke access on eBay).
func (s *Service) Disconnect(ctx context.Context, accountID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM account_ebay WHERE account_id = $1`, accountID)
	return err
}

// AccessToken is a working access token for the seller, refreshed when
// it's about to run out (eBay's last two hours).
func (s *Service) AccessToken(ctx context.Context, accountID string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var refreshBox, accessBox []byte
	var accessExp sql.NullTime
	var refreshExp time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT refresh_enc, refresh_expires_at, access_enc, access_expires_at FROM account_ebay WHERE account_id = $1`,
		accountID).Scan(&refreshBox, &refreshExp, &accessBox, &accessExp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", err
	}
	now := s.now()
	if !refreshExp.After(now) {
		return "", ErrReconnect
	}
	if accessBox != nil && accessExp.Valid && accessExp.Time.After(now.Add(5*time.Minute)) {
		if a, err := s.Sealer.OpenFor(purpose, accessBox); err == nil {
			return string(a), nil
		}
	}
	refresh, err := s.Sealer.OpenFor(purpose, refreshBox)
	if err != nil {
		return "", err
	}
	t, err := s.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {string(refresh)}, "scope": {strings.Join(Scopes, " ")}})
	if errors.Is(err, ErrReconnect) {
		_, _ = s.DB.ExecContext(ctx, `UPDATE account_ebay SET refresh_expires_at = $2 WHERE account_id = $1`, accountID, now)
		return "", ErrReconnect
	}
	if err != nil {
		return "", err
	}
	box, err := s.Sealer.SealFor(purpose, []byte(t.AccessToken))
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE account_ebay SET access_enc = $2, access_expires_at = $3, updated_at = $4 WHERE account_id = $1`,
		accountID, box, now.Add(time.Duration(t.ExpiresIn)*time.Second), now)
	return t.AccessToken, err
}
