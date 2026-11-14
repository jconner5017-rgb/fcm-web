// Package backoffice is the staff side of FireCardManager Web, like Slabs &
// Singles' admin: an overview, members with their sign-ins, PCs, notes and
// history, suspicious-activity alerts, and an audit log of staff actions.
// Only owners (ADMIN_EMAILS) and the staff they grant a role to can use it.
package backoffice

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
)

var ErrNotFound = errors.New("not found")

type Service struct {
	DB   *sql.DB
	Auth *auth.Service
}

// ---- audit ----

// Audit records a staff action.
func (s *Service) Audit(ctx context.Context, actor auth.Account, action, accountID, detail string) {
	var email sql.NullString
	if accountID != "" {
		s.DB.QueryRowContext(ctx, `SELECT email FROM accounts WHERE id::text = $1`, accountID).Scan(&email)
	}
	var acct any
	if accountID != "" {
		acct = accountID
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	s.DB.ExecContext(ctx, `INSERT INTO admin_audit (actor_id, actor_email, action, account_id, account_email, detail)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`, actor.ID, actor.Email, action, acct, email, detail)
}

type AuditEntry struct {
	ID           int64     `json:"id"`
	ActorEmail   string    `json:"actor_email"`
	Action       string    `json:"action"`
	AccountID    string    `json:"account_id,omitempty"`
	AccountEmail string    `json:"account_email,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	At           time.Time `json:"at"`
}

// AuditLog is the newest staff actions, optionally for one account.
func (s *Service) AuditLog(ctx context.Context, accountID string, limit int) ([]AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, actor_email, action, coalesce(account_id::text, ''), coalesce(account_email, ''),
			coalesce(detail, ''), created_at
		FROM admin_audit WHERE $1 = '' OR account_id::text = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorEmail, &e.Action, &e.AccountID, &e.AccountEmail, &e.Detail, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- overview ----

type Overview struct {
	Accounts        int `json:"accounts"`
	OnTrial         int `json:"on_trial"`
	Paid            int `json:"paid"`
	Bundle          int `json:"bundle"`
	Off             int `json:"off"`
	Suspended       int `json:"suspended"`
	Closing         int `json:"closing"`
	Signups1        int `json:"signups_1"`
	Signups7        int `json:"signups_7"`
	Signups30       int `json:"signups_30"`
	BlockedSignups7 int `json:"blocked_signups_7"`
	FailedSignIns1  int `json:"failed_sign_ins_1"`
	TrialsReused30  int `json:"trials_reused_30"`
	LinkedPCs       int `json:"linked_pcs"`
	Cards           int `json:"cards"`
	Photos          int `json:"photos"`
	ExpiringIn14    int `json:"expiring_in_14"`
	OpenAlerts      int `json:"open_alerts"`
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	err := s.DB.QueryRowContext(ctx, `SELECT
			count(*),
			count(*) FILTER (WHERE status = 'active' AND web_addon_until IS NULL AND sync_trial_ends_at > now()),
			count(*) FILTER (WHERE web_addon_until > now()),
			count(*) FILTER (WHERE web_addon_until > now() AND web_plan = 'bundle'),
			count(*) FILTER (WHERE status = 'active' AND coalesce(web_addon_until < now(), true) AND sync_trial_ends_at <= now() AND NOT is_admin),
			count(*) FILTER (WHERE status = 'disabled' AND closing_at IS NULL),
			count(*) FILTER (WHERE closing_at IS NOT NULL),
			count(*) FILTER (WHERE created_at > now() - interval '1 day'),
			count(*) FILTER (WHERE created_at > now() - interval '7 days'),
			count(*) FILTER (WHERE created_at > now() - interval '30 days'),
			count(*) FILTER (WHERE trial_ended_reason IN ('email', 'pc', 'inventory') AND updated_at > now() - interval '30 days'),
			count(*) FILTER (WHERE web_addon_until BETWEEN now() AND now() + interval '14 days')
		FROM accounts`).Scan(&o.Accounts, &o.OnTrial, &o.Paid, &o.Bundle, &o.Off, &o.Suspended, &o.Closing,
		&o.Signups1, &o.Signups7, &o.Signups30, &o.TrialsReused30, &o.ExpiringIn14)
	if err != nil {
		return o, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM login_events WHERE reason LIKE 'signup_blocked%' AND created_at > now() - interval '7 days'),
			(SELECT count(*) FROM login_events WHERE NOT success AND coalesce(reason, '') NOT LIKE 'signup%' AND coalesce(reason, '') NOT LIKE 'banned%' AND created_at > now() - interval '1 day'),
			(SELECT count(*) FROM devices WHERE revoked_at IS NULL),
			(SELECT count(*) FROM sync_cards WHERE deleted_at IS NULL),
			(SELECT count(*) FROM sync_photos)`).Scan(&o.BlockedSignups7, &o.FailedSignIns1, &o.LinkedPCs, &o.Cards, &o.Photos); err != nil {
		return o, err
	}
	al, err := s.Alerts(ctx)
	if err != nil {
		return o, err
	}
	o.OpenAlerts = len(al)
	return o, nil
}

// ---- members ----

// Filters for the member list.
var Filters = map[string]string{
	"":           "",
	"trial":      `a.status = 'active' AND a.web_addon_until IS NULL AND a.sync_trial_ends_at > now()`,
	"paid":       `a.web_addon_until > now()`,
	"off":        `a.status = 'active' AND coalesce(a.web_addon_until < now(), true) AND a.sync_trial_ends_at <= now() AND NOT a.is_admin`,
	"suspended":  `a.status = 'disabled' AND a.closing_at IS NULL`,
	"closing":    `a.closing_at IS NOT NULL`,
	"reused":     `a.trial_ended_reason IN ('email', 'pc', 'inventory')`,
	"staff":      `(a.is_admin OR a.staff_role IS NOT NULL)`,
	"banned":     `a.banned_at IS NOT NULL`,
	"unverified": `a.email_verified_at IS NULL`,
}

type Device struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Edition  string     `json:"edition"`
	Version  string     `json:"app_version,omitempty"`
	Created  time.Time  `json:"created_at"`
	LastSeen *time.Time `json:"last_seen_at,omitempty"`
	Revoked  *time.Time `json:"revoked_at,omitempty"`
	Shared   int        `json:"shared_with"` // other accounts this PC is linked to
}

type SignIn struct {
	At      time.Time `json:"at"`
	IP      string    `json:"ip"`
	Agent   string    `json:"user_agent"`
	Success bool      `json:"success"`
	Reason  string    `json:"reason,omitempty"`
}

type Note struct {
	ID     int64     `json:"id"`
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

type Related struct {
	ID     string `json:"id,omitempty"` // "" when that account has been deleted
	Email  string `json:"email,omitempty"`
	Reason string `json:"reason"` // same signup network, same PC, same mailbox, same inventory
}

type Member struct {
	auth.AdminRow
	SignupIP  string       `json:"signup_ip,omitempty"`
	Closing   *time.Time   `json:"closing_at,omitempty"`
	MFA       bool         `json:"mfa_enabled"`
	Sessions  int          `json:"sessions"`
	Devices   []Device     `json:"devices"`
	SignIns   []SignIn     `json:"sign_ins"`
	Notes     []Note       `json:"notes"`
	Audit     []AuditEntry `json:"audit"`
	Related   []Related    `json:"related"`
	BannedAt  *time.Time   `json:"banned_at,omitempty"`
	SoldCount int          `json:"sold"`
}

func (s *Service) Member(ctx context.Context, id string) (Member, error) {
	var m Member
	list, err := s.Auth.ListAccounts(ctx, "", "id:"+id, 1)
	if err != nil {
		return m, err
	}
	if len(list) == 0 {
		return m, ErrNotFound
	}
	m.AdminRow = list[0]
	var ip sql.NullString
	var closing, banned sql.NullTime
	if err := s.DB.QueryRowContext(ctx, `SELECT signup_ip, closing_at, banned_at,
			EXISTS (SELECT 1 FROM account_mfa f WHERE f.account_id = a.id AND f.confirmed_at IS NOT NULL),
			(SELECT count(*) FROM sessions x WHERE x.account_id = a.id AND x.expires_at > now()),
			(SELECT count(*) FROM sync_cards c WHERE c.account_id = a.id AND c.deleted_at IS NULL AND c.status = 'Sold')
		FROM accounts a WHERE a.id = $1`, m.ID).Scan(&ip, &closing, &banned, &m.MFA, &m.Sessions, &m.SoldCount); err != nil {
		return m, err
	}
	m.SignupIP = ip.String
	if banned.Valid {
		t := banned.Time
		m.BannedAt = &t
	}
	if closing.Valid {
		t := closing.Time
		m.Closing = &t
	}

	m.Devices = []Device{}
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id, d.name, d.edition, coalesce(d.app_version, ''), d.created_at, d.last_seen_at, d.revoked_at,
			(SELECT count(DISTINCT o.account_id) FROM devices o WHERE upper(o.hwid) = upper(d.hwid) AND o.account_id <> d.account_id)
		FROM devices d WHERE d.account_id = $1 ORDER BY d.revoked_at NULLS FIRST, d.created_at DESC`, m.ID)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var d Device
		var seen, rev sql.NullTime
		if err := rows.Scan(&d.ID, &d.Name, &d.Edition, &d.Version, &d.Created, &seen, &rev, &d.Shared); err != nil {
			rows.Close()
			return m, err
		}
		if seen.Valid {
			t := seen.Time
			d.LastSeen = &t
		}
		if rev.Valid {
			t := rev.Time
			d.Revoked = &t
		}
		m.Devices = append(m.Devices, d)
	}
	rows.Close()

	m.SignIns = []SignIn{}
	rows, err = s.DB.QueryContext(ctx, `SELECT created_at, coalesce(ip, ''), coalesce(user_agent, ''), success, coalesce(reason, '')
		FROM login_events WHERE account_id = $1 OR (account_id IS NULL AND lower(email) = lower($2))
		ORDER BY created_at DESC LIMIT 50`, m.ID, m.Email)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var e SignIn
		if err := rows.Scan(&e.At, &e.IP, &e.Agent, &e.Success, &e.Reason); err != nil {
			rows.Close()
			return m, err
		}
		m.SignIns = append(m.SignIns, e)
	}
	rows.Close()

	m.Notes = []Note{}
	rows, err = s.DB.QueryContext(ctx, `SELECT id, author_email, body, created_at FROM admin_notes WHERE account_id = $1 ORDER BY created_at DESC LIMIT 100`, m.ID)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Author, &n.Body, &n.At); err != nil {
			rows.Close()
			return m, err
		}
		m.Notes = append(m.Notes, n)
	}
	rows.Close()

	if m.Audit, err = s.AuditLog(ctx, m.ID, 50); err != nil {
		return m, err
	}
	m.Related, err = s.related(ctx, m.ID, m.SignupIP)
	return m, err
}

// related lists other accounts tied to this one: same sign-up network, same
// PC, same mailbox, or the same inventory (from the free-trial records).
func (s *Service) related(ctx context.Context, id, ip string) ([]Related, error) {
	out := []Related{}
	seen := map[string]bool{}
	add := func(rid, email, reason string) {
		k := rid + "|" + reason
		if rid == id || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, Related{ID: rid, Email: email, Reason: reason})
	}
	q := func(reason, query string, args ...any) error {
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rid, email sql.NullString
			if err := rows.Scan(&rid, &email); err != nil {
				return err
			}
			add(rid.String, email.String, reason)
		}
		return rows.Err()
	}
	if ip != "" {
		if err := q("same sign-up network", `SELECT id::text, email FROM accounts WHERE signup_ip = $1 AND id <> $2 LIMIT 25`, ip, id); err != nil {
			return nil, err
		}
	}
	if err := q("same PC", `SELECT DISTINCT o.account_id::text, a.email FROM devices d JOIN devices o ON upper(o.hwid) = upper(d.hwid) AND o.account_id <> d.account_id
		JOIN accounts a ON a.id = o.account_id WHERE d.account_id = $1 LIMIT 25`, id); err != nil {
		return nil, err
	}
	for _, kind := range []string{"email", "hwid"} {
		reason := map[string]string{"email": "same mailbox", "hwid": "same PC"}[kind]
		if err := q(reason, `SELECT DISTINCT o.account_ref::text, a.email FROM trial_claims t JOIN trial_claims o ON o.kind = t.kind AND o.value = t.value AND o.account_ref <> t.account_ref
			LEFT JOIN accounts a ON a.id = o.account_ref WHERE t.account_ref = $1 AND t.kind = $2 LIMIT 25`, id, kind); err != nil {
			return nil, err
		}
	}
	if err := q("same inventory", `SELECT o.account_ref::text, max(a.email) FROM trial_card_marks t JOIN trial_card_marks o ON o.fp = t.fp AND o.account_ref <> t.account_ref
		LEFT JOIN accounts a ON a.id = o.account_ref WHERE t.account_ref = $1 GROUP BY o.account_ref HAVING count(*) >= 25 LIMIT 25`, id); err != nil {
		return nil, err
	}
	// Deleted accounts have no id to open; show them without one.
	for i := range out {
		if out[i].Email == "" {
			out[i].ID = ""
			out[i].Email = "(closed account)"
		}
	}
	return out, nil
}

// ---- actions ----

// Suspend signs the account out everywhere and stops sign-in and syncing.
func (s *Service) Suspend(ctx context.Context, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 300 {
		reason = reason[:300]
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET status = 'disabled', suspended_reason = $2, updated_at = now()
		WHERE id::text = $1 AND closing_at IS NULL AND NOT is_admin`, id, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE account_id::text = $1`, id)
	return err
}

func (s *Service) Unsuspend(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET status = 'active', suspended_reason = NULL, updated_at = now()
		WHERE id::text = $1 AND closing_at IS NULL AND status = 'disabled'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SignOut ends every session the account has.
func (s *Service) SignOut(ctx context.Context, id string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE account_id::text = $1`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) AddNote(ctx context.Context, id, author, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 4000 {
		return &auth.ValidationError{Fields: map[string]string{"body": "Write a note (up to 4,000 characters)."}}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO admin_notes (account_id, author_email, body)
		SELECT id, $2, $3 FROM accounts WHERE id::text = $1`, id, author, body)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- staff ----

type Staff struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"` // owner, manager, support
	MFA   bool   `json:"mfa_enabled"`
}

func (s *Service) StaffList(ctx context.Context) ([]Staff, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, email, CASE WHEN is_admin THEN 'owner' ELSE staff_role END,
			EXISTS (SELECT 1 FROM account_mfa f WHERE f.account_id = a.id AND f.confirmed_at IS NOT NULL)
		FROM accounts a WHERE is_admin OR staff_role IS NOT NULL ORDER BY is_admin DESC, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Staff{}
	for rows.Next() {
		var x Staff
		if err := rows.Scan(&x.ID, &x.Email, &x.Role, &x.MFA); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SetStaff gives an existing account a back-office role ("" takes it away).
// Owners are set by ADMIN_EMAILS and can't be changed here.
func (s *Service) SetStaff(ctx context.Context, email, role string) (string, error) {
	if role != "" && role != "support" && role != "manager" {
		return "", &auth.ValidationError{Fields: map[string]string{"role": "Use support or manager."}}
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `UPDATE accounts SET staff_role = NULLIF($2, ''), updated_at = now()
		WHERE lower(email) = lower($1) AND NOT is_admin RETURNING id`, strings.TrimSpace(email), role).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
