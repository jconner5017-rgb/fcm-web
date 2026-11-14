// Package trial stops the 30-day free trial of FireCardManager Web being
// used over and over by the same seller: a new account's trial ends early
// when it's tied to an earlier account by its email (normalized, so
// name+1@gmail.com and n.a.m.e@gmail.com count as name@gmail.com), a linked
// PC (its hardware id), or its inventory (most of its cards are the same
// cards an earlier account had). Viewing stays free; syncing, editing and
// listing need the paid plan. Records survive account deletion.
package trial

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
)

// Inventory matching: at least MinMatch cards, and at least MatchShare of
// the new account's cards, the same as one earlier account's.
const (
	MinMatch   = 25
	MatchShare = 0.5
	maxMarks   = 10000
)

// Reasons (accounts.trial_ended_reason), shown to the seller and in Admin.
// An admin extending the trial sets "allowed", which no check overrides.
const (
	ReasonEmail     = "email"
	ReasonPC        = "pc"
	ReasonInventory = "inventory"
)

type Service struct{ DB *sql.DB }

// NormEmail is the mailbox an address really goes to: lower case, no
// +tag, and for Gmail no dots (and googlemail.com is gmail.com).
func NormEmail(email string) string {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(e, "@")
	if at < 1 {
		return e
	}
	local, domain := e[:at], e[at+1:]
	if i := strings.IndexByte(local, '+'); i > 0 {
		local = local[:i]
	}
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	return local + "@" + domain
}

func hash(kind, v string) []byte {
	h := sha256.Sum256([]byte("fcm-trial|" + kind + "|" + v))
	return h[:]
}

type acct struct {
	id, email string
	created   sql.NullTime
	inTrial   bool // on the free trial now, not paid, not admin
}

func (s *Service) load(ctx context.Context, accountID string) (acct, error) {
	var a acct
	err := s.DB.QueryRowContext(ctx, `SELECT id, email, created_at,
			sync_trial_ends_at > now() AND NOT is_admin AND web_addon_until IS NULL
		FROM accounts WHERE id = $1`, accountID).Scan(&a.id, &a.email, &a.created, &a.inTrial)
	return a, err
}

// End stops an account's free trial now (only while it's on the trial and unpaid).
func (s *Service) End(ctx context.Context, accountID, reason string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET sync_trial_ends_at = now(), trial_ended_reason = $2, updated_at = now()
		WHERE id = $1 AND sync_trial_ends_at > now() AND NOT is_admin AND web_addon_until IS NULL
		  AND trial_ended_reason IS NULL`, accountID, reason) // an admin's extension ("allowed") sticks
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// claim records a value for the account and reports whether an earlier
// account already had it.
func (s *Service) claim(ctx context.Context, a acct, kind, value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	v := hash(kind, value)
	var earlier bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM trial_claims
		WHERE kind = $1 AND value = $2 AND account_ref <> $3 AND claimed_at < $4)`, kind, v, a.id, a.created.Time).Scan(&earlier); err != nil {
		return false, err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO trial_claims (kind, value, account_ref, claimed_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, kind, v, a.id, a.created.Time)
	return earlier, err
}

// Check looks at everything the account is tied to and ends its trial when
// an earlier account had the same email, PC or inventory. Returns the reason
// ("" when the trial goes on, or the account isn't on one).
func (s *Service) Check(ctx context.Context, accountID string) (string, error) {
	a, err := s.load(ctx, accountID)
	if err != nil {
		return "", err
	}
	end := func(reason string) (string, error) {
		if !a.inTrial {
			return "", nil
		}
		ok, err := s.End(ctx, a.id, reason)
		if err != nil || !ok {
			return "", err
		}
		return reason, nil
	}

	if reused, err := s.claim(ctx, a, "email", NormEmail(a.email)); err != nil {
		return "", err
	} else if reused {
		if r, err := end(ReasonEmail); r != "" || err != nil {
			return r, err
		}
	}

	rows, err := s.DB.QueryContext(ctx, `SELECT hwid FROM devices WHERE account_id = $1
		UNION SELECT hwid FROM device_links WHERE approved_by = $1`, a.id)
	if err != nil {
		return "", err
	}
	var hwids []string
	for rows.Next() {
		var h string
		if rows.Scan(&h) == nil {
			hwids = append(hwids, strings.ToUpper(strings.TrimSpace(h)))
		}
	}
	rows.Close()
	for _, h := range hwids {
		if reused, err := s.claim(ctx, a, "hwid", h); err != nil {
			return "", err
		} else if reused {
			if r, err := end(ReasonPC); r != "" || err != nil {
				return r, err
			}
		}
	}
	return s.checkInventory(ctx, a, end)
}

// cardFP is the SQL for a card's fingerprint (identity, not location or price).
const cardFP = `decode(md5(lower(concat_ws('|', trim(data->>'player_name'), data->>'year', trim(data->>'set_name'),
	trim(data->>'card_number'), trim(coalesce(data->>'variety', ''))))), 'hex')`

func (s *Service) checkInventory(ctx context.Context, a acct, end func(string) (string, error)) (string, error) {
	var n, marked int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*), (SELECT trial_marked_cards FROM accounts WHERE id = $1)
		FROM sync_cards WHERE account_id = $1 AND deleted_at IS NULL AND coalesce(data->>'player_name', '') <> ''
		  AND coalesce(data->>'set_name', '') <> ''`, a.id).Scan(&n, &marked); err != nil {
		return "", err
	}
	if n == marked || n == 0 {
		return "", nil // nothing new since the last look
	}
	if a.inTrial && n >= MinMatch {
		var best int
		err := s.DB.QueryRowContext(ctx, `WITH mine AS (
				SELECT DISTINCT `+cardFP+` AS fp FROM sync_cards
				WHERE account_id = $1 AND deleted_at IS NULL AND coalesce(data->>'player_name', '') <> '' AND coalesce(data->>'set_name', '') <> ''
				LIMIT $3)
			SELECT coalesce(max(c), 0) FROM (SELECT count(*) AS c FROM trial_card_marks m JOIN mine USING (fp)
				WHERE m.account_ref <> $1 AND m.account_created < $2 GROUP BY m.account_ref) t`,
			a.id, a.created.Time, maxMarks).Scan(&best)
		if err != nil {
			return "", err
		}
		var distinct int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(DISTINCT `+cardFP+`) FROM sync_cards WHERE account_id = $1 AND deleted_at IS NULL
			AND coalesce(data->>'player_name', '') <> '' AND coalesce(data->>'set_name', '') <> ''`, a.id).Scan(&distinct); err != nil {
			return "", err
		}
		if best >= MinMatch && float64(best) >= MatchShare*float64(min(distinct, maxMarks)) {
			return end(ReasonInventory)
		}
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO trial_card_marks (account_ref, fp, account_created)
		SELECT DISTINCT $1::uuid, `+cardFP+`, $2::timestamptz FROM sync_cards
		WHERE account_id = $1 AND deleted_at IS NULL AND coalesce(data->>'player_name', '') <> '' AND coalesce(data->>'set_name', '') <> ''
		LIMIT $3
		ON CONFLICT DO NOTHING`, a.id, a.created.Time, maxMarks); err != nil {
		return "", err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET trial_marked_cards = $2 WHERE id = $1`, a.id, n)
	return "", err
}

// Sweep checks every account whose cards changed since its last look (and
// records the ones not on a trial, so a later account can be matched to
// them). Runs every few minutes.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id FROM accounts a
		WHERE a.status = 'active' AND a.trial_marked_cards <> (SELECT count(*) FROM sync_cards c WHERE c.account_id = a.id AND c.deleted_at IS NULL
			AND coalesce(c.data->>'player_name', '') <> '' AND coalesce(c.data->>'set_name', '') <> '')
		   OR NOT EXISTS (SELECT 1 FROM trial_claims t WHERE t.account_ref = a.id AND t.kind = 'email')
		ORDER BY a.created_at LIMIT 200`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	ended := 0
	for _, id := range ids {
		r, err := s.Check(ctx, id)
		if err != nil {
			return ended, err
		}
		if r != "" {
			ended++
		}
	}
	return ended, nil
}
