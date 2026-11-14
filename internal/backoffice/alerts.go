package backoffice

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Alert is suspicious activity worth a look.
type Alert struct {
	Key          string    `json:"key"`
	Kind         string    `json:"kind"`
	Level        string    `json:"level"` // high, medium, low
	Title        string    `json:"title"`
	Detail       string    `json:"detail"`
	AccountID    string    `json:"account_id,omitempty"`
	AccountEmail string    `json:"account_email,omitempty"`
	IP           string    `json:"ip,omitempty"`
	Count        int       `json:"count"`
	At           time.Time `json:"at"`
}

// Thresholds for the alerts.
const (
	SignupsPerNetwork7d   = 3  // accounts made from one network in 7 days
	BlockedSignupsPerIP1d = 5  // signups stopped as bots from one network in a day
	FailedPerAccount1d    = 10 // wrong passwords on one account in a day
	FailedPerIP1d         = 20 // wrong passwords from one network in a day
	NetworksPerAccount1d  = 5  // different networks signing in to one account in a day
	AccountsPerPC         = 3  // accounts one PC is linked to
)

func (s *Service) Alerts(ctx context.Context) ([]Alert, error) {
	var out []Alert
	type q struct {
		kind, level, sql string
		build            func(a *Alert, s1, s2 string)
	}
	queries := []q{
		{"trial_reused", "medium", `SELECT id::text, email, coalesce(trial_ended_reason, ''), 1, updated_at FROM accounts
			WHERE trial_ended_reason IN ('email', 'pc', 'inventory') AND updated_at > now() - interval '30 days'`,
			func(a *Alert, id, email string) {
				a.AccountID, a.AccountEmail = id, email
			}},
		{"signup_network", "medium", fmt.Sprintf(`SELECT signup_ip, string_agg(email, ', ' ORDER BY created_at), '', count(*), max(created_at) FROM accounts
			WHERE signup_ip IS NOT NULL AND created_at > now() - interval '7 days' GROUP BY signup_ip HAVING count(*) >= %d`, SignupsPerNetwork7d),
			func(a *Alert, ip, emails string) { a.IP = ip; a.Detail = emails }},
		{"bot_signups", "low", fmt.Sprintf(`SELECT ip, string_agg(DISTINCT reason, ', '), '', count(*), max(created_at) FROM login_events
			WHERE reason LIKE 'signup_blocked%%' AND created_at > now() - interval '1 day' GROUP BY ip HAVING count(*) >= %d`, BlockedSignupsPerIP1d),
			func(a *Alert, ip, reasons string) { a.IP = ip; a.Detail = reasons }},
		{"failed_account", "high", fmt.Sprintf(`SELECT a.id::text, a.email, '', count(*), max(e.created_at) FROM login_events e JOIN accounts a ON a.id = e.account_id
			WHERE NOT e.success AND coalesce(e.reason, '') NOT LIKE 'signup%%' AND e.created_at > now() - interval '1 day'
			GROUP BY a.id, a.email HAVING count(*) >= %d`, FailedPerAccount1d),
			func(a *Alert, id, email string) { a.AccountID, a.AccountEmail = id, email }},
		{"failed_network", "high", fmt.Sprintf(`SELECT ip, string_agg(DISTINCT email, ', '), '', count(*), max(created_at) FROM login_events
			WHERE NOT success AND coalesce(reason, '') NOT LIKE 'signup%%' AND created_at > now() - interval '1 day'
			GROUP BY ip HAVING count(*) >= %d`, FailedPerIP1d),
			func(a *Alert, ip, emails string) { a.IP = ip; a.Detail = emails }},
		{"many_networks", "medium", fmt.Sprintf(`SELECT a.id::text, a.email, '', count(DISTINCT e.ip), max(e.created_at) FROM login_events e JOIN accounts a ON a.id = e.account_id
			WHERE e.success AND e.created_at > now() - interval '1 day' GROUP BY a.id, a.email HAVING count(DISTINCT e.ip) >= %d`, NetworksPerAccount1d),
			func(a *Alert, id, email string) { a.AccountID, a.AccountEmail = id, email }},
		{"shared_pc", "medium", fmt.Sprintf(`SELECT max(d.name), string_agg(DISTINCT a.email, ', '), '', count(DISTINCT d.account_id), max(d.created_at)
			FROM devices d JOIN accounts a ON a.id = d.account_id GROUP BY upper(d.hwid) HAVING count(DISTINCT d.account_id) >= %d`, AccountsPerPC),
			func(a *Alert, pc, emails string) { a.Detail = pc + ": " + emails }},
	}
	for _, x := range queries {
		rows, err := s.DB.QueryContext(ctx, x.sql)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.kind, err)
		}
		for rows.Next() {
			var s1, s2, s3 string
			a := Alert{Kind: x.kind, Level: x.level}
			if err := rows.Scan(&s1, &s2, &s3, &a.Count, &a.At); err != nil {
				rows.Close()
				return nil, err
			}
			x.build(&a, s1, s2)
			switch x.kind {
			case "trial_reused":
				a.Title = "Free trial reused: " + map[string]string{"email": "same mailbox", "pc": "same PC", "inventory": "same inventory"}[s3]
				a.Detail = "The trial ended automatically. Check it's the same seller; extend the trial if it isn't."
			case "signup_network":
				a.Title = fmt.Sprintf("%d accounts made from one network in 7 days", a.Count)
			case "bot_signups":
				a.Title = fmt.Sprintf("%d sign-ups stopped as bots from one network today", a.Count)
			case "failed_account":
				a.Title = fmt.Sprintf("%d wrong passwords on one account today", a.Count)
				a.Detail = "Someone may be guessing this account's password."
			case "failed_network":
				a.Title = fmt.Sprintf("%d wrong passwords from one network today", a.Count)
			case "many_networks":
				a.Title = fmt.Sprintf("Signed in from %d networks today", a.Count)
				a.Detail = "The account may be shared, or its password known to someone else."
			case "shared_pc":
				a.Title = fmt.Sprintf("One PC linked to %d accounts", a.Count)
			}
			a.Key = strings.Join([]string{a.Kind, a.AccountID, a.IP, a.Detail, fmt.Sprint(a.Count)}, "|")
			if len(a.Key) > 400 {
				a.Key = a.Key[:400]
			}
			out = append(out, a)
		}
		rows.Close()
	}
	// Leave out the ones staff already looked at.
	dismissed := map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key FROM admin_dismissed`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			dismissed[k] = true
		}
	}
	rows.Close()
	kept := []Alert{}
	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	for _, a := range out {
		if !dismissed[a.Key] {
			kept = append(kept, a)
		}
	}
	for i := 1; i < len(kept); i++ { // high first, then newest
		for j := i; j > 0 && (rank[kept[j].Level] < rank[kept[j-1].Level] ||
			rank[kept[j].Level] == rank[kept[j-1].Level] && kept[j].At.After(kept[j-1].At)); j-- {
			kept[j], kept[j-1] = kept[j-1], kept[j]
		}
	}
	return kept, nil
}

// Dismiss marks an alert as looked at.
func (s *Service) Dismiss(ctx context.Context, key, by string) error {
	if key == "" || len(key) > 400 {
		return ErrNotFound
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO admin_dismissed (key, by_email) VALUES ($1, $2) ON CONFLICT DO NOTHING`, key, by)
	return err
}
