package backoffice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/trial"
)

// ---- bans (like Slabs & Singles' blocklist) ----

// BanHash is how a banned identifier is stored (SQL: encode(sha256(('ban:'||kind||':'||value)::bytea), 'hex')).
func BanHash(kind, v string) string {
	h := sha256.Sum256([]byte("ban:" + kind + ":" + v))
	return hex.EncodeToString(h[:])
}

func banValue(kind, v string) string {
	switch kind {
	case "email":
		return trial.NormEmail(v)
	case "hwid":
		return strings.ToUpper(strings.TrimSpace(v))
	}
	return strings.TrimSpace(v)
}

// Banned reports whether a value belongs to a banned account (and its email).
func Banned(ctx context.Context, db *sql.DB, kind, value string) (bool, string, error) {
	v := banValue(kind, value)
	if v == "" {
		return false, "", nil
	}
	var email string
	err := db.QueryRowContext(ctx, `SELECT account_email FROM banned_identifiers WHERE kind = $1 AND value_hash = $2`, kind, BanHash(kind, v)).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	return err == nil, email, err
}

// Ban suspends the account and records its email, the networks it signed
// up and signed in from (last 90 days) and its PCs, so the same seller
// can't simply open a new account: that email can't sign up, those PCs
// can't be linked, and a sign-up from those networks raises an alert.
func (s *Service) Ban(ctx context.Context, id, reason string) error {
	if err := s.Suspend(ctx, id, "Banned: "+reason); err != nil {
		return err
	}
	var email string
	var signupIP sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT email, signup_ip FROM accounts WHERE id::text = $1`, id).Scan(&email, &signupIP); err != nil {
		return err
	}
	vals := map[string]map[string]bool{"email": {banValue("email", email): true}, "ip": {}, "hwid": {}}
	if signupIP.String != "" {
		vals["ip"][signupIP.String] = true
	}
	q := func(kind, query string) error {
		rows, err := s.DB.QueryContext(ctx, query, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			if rows.Scan(&v) == nil && strings.TrimSpace(v) != "" {
				vals[kind][banValue(kind, v)] = true
			}
		}
		return rows.Err()
	}
	if err := q("ip", `SELECT DISTINCT ip FROM login_events WHERE account_id::text = $1 AND success AND ip IS NOT NULL AND created_at > now() - interval '90 days'`); err != nil {
		return err
	}
	if err := q("hwid", `SELECT hwid FROM devices WHERE account_id::text = $1`); err != nil {
		return err
	}
	for kind, set := range vals {
		for v := range set {
			if _, err := s.DB.ExecContext(ctx, `INSERT INTO banned_identifiers (kind, value_hash, account_ref, account_email)
				VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, kind, BanHash(kind, v), id, email); err != nil {
				return err
			}
		}
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET banned_at = now() WHERE id::text = $1`, id)
	return err
}

// Unban lifts a ban: its records are removed and the account can sign in again.
func (s *Service) Unban(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM banned_identifiers WHERE account_ref::text = $1`, id); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE accounts SET banned_at = NULL WHERE id::text = $1`, id); err != nil {
		return err
	}
	return s.Unsuspend(ctx, id)
}

// ---- back office network allowlist (like Slabs & Singles) ----

type IPRule struct {
	ID        int64     `json:"id"`
	CIDR      string    `json:"cidr"`
	Label     string    `json:"label"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

var ErrLockout = errors.New("that would block the network you're on now; add your current network first")

// IPRules: with no rules any network may use the back office (2FA still
// applies). Disabled (BACKOFFICE_IP_ALLOWLIST=off) is the break-glass switch.
type IPRules struct {
	DB       *sql.DB
	Disabled bool

	mu     sync.Mutex
	cached []*net.IPNet
	at     time.Time
}

func (p *IPRules) Allowed(ctx context.Context, ip string) (bool, error) {
	if p.Disabled {
		return true, nil
	}
	p.mu.Lock()
	nets, fresh := p.cached, p.cached != nil && time.Since(p.at) < 15*time.Second
	p.mu.Unlock()
	if !fresh {
		rules, err := p.List(ctx)
		if err != nil {
			return false, err
		}
		nets = toNets(rules)
		p.mu.Lock()
		p.cached, p.at = nets, time.Now()
		p.mu.Unlock()
	}
	return inNets(nets, net.ParseIP(ip)), nil
}

func toNets(rules []IPRule) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(rules))
	for _, r := range rules {
		if _, n, err := net.ParseCIDR(r.CIDR); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

func inNets(nets []*net.IPNet, ip net.IP) bool {
	if len(nets) == 0 {
		return true
	}
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (p *IPRules) List(ctx context.Context) ([]IPRule, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id, cidr, label, created_by, created_at FROM admin_ip_rules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IPRule{}
	for rows.Next() {
		var r IPRule
		if err := rows.Scan(&r.ID, &r.CIDR, &r.Label, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NormCIDR accepts an address (one network) or a CIDR range.
func NormCIDR(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !strings.Contains(v, "/") {
		ip := net.ParseIP(v)
		if ip == nil {
			return "", false
		}
		if ip.To4() != nil {
			return ip.String() + "/32", true
		}
		return ip.String() + "/128", true
	}
	_, n, err := net.ParseCIDR(v)
	if err != nil {
		return "", false
	}
	return n.String(), true
}

// Add adds a network; the first rule must include the caller's own network.
func (p *IPRules) Add(ctx context.Context, cidr, label, by, currentIP string) (IPRule, error) {
	c, ok := NormCIDR(cidr)
	if !ok {
		return IPRule{}, errors.New("bad network")
	}
	rules, err := p.List(ctx)
	if err != nil {
		return IPRule{}, err
	}
	if !p.Disabled && !inNets(toNets(append(rules, IPRule{CIDR: c})), net.ParseIP(currentIP)) {
		return IPRule{}, ErrLockout
	}
	var r IPRule
	err = p.DB.QueryRowContext(ctx, `INSERT INTO admin_ip_rules (cidr, label, created_by) VALUES ($1, $2, $3)
		ON CONFLICT (cidr) DO UPDATE SET label = EXCLUDED.label RETURNING id, cidr, label, created_by, created_at`,
		c, strings.TrimSpace(label), by).Scan(&r.ID, &r.CIDR, &r.Label, &r.CreatedBy, &r.CreatedAt)
	p.mu.Lock()
	p.cached = nil
	p.mu.Unlock()
	return r, err
}

// Remove deletes a rule unless that would lock the caller out.
func (p *IPRules) Remove(ctx context.Context, id int64, currentIP string) error {
	rules, err := p.List(ctx)
	if err != nil {
		return err
	}
	var left []IPRule
	for _, r := range rules {
		if r.ID != id {
			left = append(left, r)
		}
	}
	if !p.Disabled && len(left) > 0 && !inNets(toNets(left), net.ParseIP(currentIP)) {
		return ErrLockout
	}
	_, err = p.DB.ExecContext(ctx, `DELETE FROM admin_ip_rules WHERE id = $1`, id)
	p.mu.Lock()
	p.cached = nil
	p.mu.Unlock()
	return err
}
