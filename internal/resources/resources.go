// Package resources is the back office's resource monitor: how full the
// database disk, memory, CPU and connections are, how much photo storage is
// used, and how fast it's growing, with a plain "upgrade this" note when
// something gets close to its limit. The site's own numbers always show;
// CPU and memory for each Render service show when RENDER_API_KEY is set.
package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gauge is one thing that can run out.
type Gauge struct {
	Key     string  `json:"key"`
	Group   string  `json:"group"` // Database, API server, Website server, Photo storage
	Label   string  `json:"label"`
	Used    float64 `json:"used"`
	Limit   float64 `json:"limit,omitempty"` // 0: no hard limit (pay as you go)
	Unit    string  `json:"unit"`            // bytes, cpu, count
	Percent float64 `json:"percent,omitempty"`
	Level   string  `json:"level"` // ok, warn (70%+), high (85%+), info
	Note    string  `json:"note,omitempty"`
	Upgrade string  `json:"upgrade,omitempty"` // what to do when it's high
	Peak    bool    `json:"peak,omitempty"`    // Used is the highest in the last 24 hours
}

type Table struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Rows  int64  `json:"rows"`
}

type Growth struct {
	Days           float64  `json:"days"` // span the rates are measured over
	DBBytesPerDay  float64  `json:"db_bytes_per_day"`
	PhotoBytesDay  float64  `json:"photo_bytes_per_day"`
	CardsPerDay    float64  `json:"cards_per_day"`
	AccountsPerDay float64  `json:"accounts_per_day"`
	DBFullInDays   *float64 `json:"db_full_in_days,omitempty"`
}

type Report struct {
	At       time.Time        `json:"at"`
	Gauges   []Gauge          `json:"gauges"`
	Counts   map[string]int64 `json:"counts"`
	Tables   []Table          `json:"tables"`
	Growth   *Growth          `json:"growth,omitempty"`
	Render   string           `json:"render"` // "on", "off" (no key) or the error
	Uptime   string           `json:"uptime"`
	Snapshot []Snapshot       `json:"history"`
}

type Snapshot struct {
	At         time.Time `json:"at"`
	DBBytes    int64     `json:"db_bytes"`
	PhotoBytes int64     `json:"photo_bytes"`
	Photos     int64     `json:"photos"`
	Cards      int64     `json:"cards"`
	Accounts   int64     `json:"accounts"`
}

type Service struct {
	DB        *sql.DB
	RenderKey string
	// Render ids: the API server, the website server and the database.
	APIID, WebID, DBID string
	DBDiskGB           float64 // the database disk size (from Render when the key is set)
	HTTP               *http.Client
	Started            time.Time
	renderURL          string

	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

const (
	warnAt = 70.0
	highAt = 85.0
)

func level(p float64) string {
	switch {
	case p >= highAt:
		return "high"
	case p >= warnAt:
		return "warn"
	}
	return "ok"
}

func (g *Gauge) fill() {
	if g.Limit > 0 {
		g.Percent = g.Used / g.Limit * 100
		g.Level = level(g.Percent)
	} else if g.Level == "" {
		g.Level = "info"
	}
}

// Report gathers everything (cached for a minute: the Render calls are slow).
func (s *Service) Report(ctx context.Context) (Report, error) {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.cachedAt) < time.Minute {
		r := *s.cached
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	r, err := s.build(ctx)
	if err != nil {
		return r, err
	}
	s.mu.Lock()
	s.cached, s.cachedAt = &r, time.Now()
	s.mu.Unlock()
	return r, nil
}

func (s *Service) build(ctx context.Context) (Report, error) {
	r := Report{At: time.Now().UTC(), Counts: map[string]int64{}, Render: "off"}
	if !s.Started.IsZero() {
		r.Uptime = time.Since(s.Started).Round(time.Minute).String()
	}

	// ---- the database ----
	var dbBytes, conns, maxConns int64
	if err := s.DB.QueryRowContext(ctx, `SELECT pg_database_size(current_database()),
		(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
		current_setting('max_connections')::int`).Scan(&dbBytes, &conns, &maxConns); err != nil {
		return r, err
	}
	disk := s.DBDiskGB
	var render *renderClient
	if s.RenderKey != "" {
		render = &renderClient{key: s.RenderKey, http: s.httpClient(), base: s.base()}
		if s.DBID != "" {
			if gb, err := render.diskGB(ctx, s.DBID); err == nil && gb > 0 {
				disk = gb
			}
		}
	}
	if disk <= 0 {
		disk = 1
	}
	r.Gauges = append(r.Gauges,
		Gauge{Key: "db_disk", Group: "Database", Label: "Disk", Used: float64(dbBytes), Limit: disk * 1e9, Unit: "bytes",
			Note:    fmt.Sprintf("Everything the site stores except photos. %g GB disk.", disk),
			Upgrade: "Render > fcm-db > Settings: raise the disk size (it can't be lowered again), or turn on disk autoscaling."},
		Gauge{Key: "db_connections", Group: "Database", Label: "Connections", Used: float64(conns), Limit: float64(maxConns), Unit: "count",
			Note:    "Open connections right now.",
			Upgrade: "Render > fcm-db: a bigger plan allows more connections."},
	)

	// ---- photos (Cloudflare R2: no limit, billed per GB) ----
	var photoBytes, photos int64
	if err := s.DB.QueryRowContext(ctx, `SELECT coalesce(sum(bytes), 0), count(*) FROM sync_photos WHERE status = 'ready'`).Scan(&photoBytes, &photos); err != nil {
		return r, err
	}
	gb := float64(photoBytes) / 1e9
	cost := 0.0
	if gb > 10 {
		cost = (gb - 10) * 0.015
	}
	r.Gauges = append(r.Gauges, Gauge{Key: "photos", Group: "Photo storage", Label: "Cloudflare R2", Used: float64(photoBytes), Unit: "bytes",
		Note: fmt.Sprintf("%s photos. R2 has no limit: the first 10 GB are free, then $0.015 per GB a month (about $%.2f a month now).",
			strconv.FormatInt(photos, 10), cost)})
	r.Counts["photos"] = photos

	// ---- counts ----
	for k, q := range map[string]string{
		"accounts":        `SELECT count(*) FROM accounts`,
		"active_30_days":  `SELECT count(DISTINCT account_id) FROM (SELECT account_id FROM login_events WHERE success AND created_at > now() - interval '30 days' UNION SELECT account_id FROM devices WHERE last_seen_at > now() - interval '30 days') x WHERE account_id IS NOT NULL`,
		"linked_pcs":      `SELECT count(*) FROM devices WHERE revoked_at IS NULL`,
		"cards":           `SELECT count(*) FROM sync_cards WHERE deleted_at IS NULL AND coalesce(status, '') <> 'Sold'`,
		"sold":            `SELECT count(*) FROM sync_sold WHERE deleted_at IS NULL`,
		"email_waiting":   `SELECT count(*) FROM email_outbox WHERE sent_at IS NULL`,
		"listing_waiting": `SELECT count(*) FROM list_job_items WHERE status = 'pending'`,
	} {
		var n int64
		if err := s.DB.QueryRowContext(ctx, q).Scan(&n); err == nil {
			r.Counts[k] = n
		}
	}

	// ---- biggest tables ----
	rows, err := s.DB.QueryContext(ctx, `SELECT c.relname, pg_total_relation_size(c.oid), coalesce(s.n_live_tup, 0)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE c.relkind = 'r' AND n.nspname = 'public' ORDER BY 2 DESC LIMIT 8`)
	if err == nil {
		for rows.Next() {
			var t Table
			if rows.Scan(&t.Name, &t.Bytes, &t.Rows) == nil {
				r.Tables = append(r.Tables, t)
			}
		}
		rows.Close()
	}

	// ---- the API server itself (always) ----
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	apiMem := Gauge{Key: "api_memory", Group: "API server", Label: "Memory", Used: float64(ms.Sys), Unit: "bytes",
		Note:    "This server's memory right now.",
		Upgrade: "Render > fcm-api > Settings > Instance type: pick the next size up (Standard = 2 GB)."}
	if used, limit, ok := cgroupMemory(); ok {
		apiMem.Used, apiMem.Limit = float64(used), float64(limit)
	}

	// ---- Render: CPU and memory of each server, database CPU/memory (last 24 hours, peak) ----
	var renderGauges []Gauge
	if render != nil {
		r.Render = "on"
		for _, t := range []struct{ id, group, key, upgrade string }{
			{s.APIID, "API server", "api", "Render > fcm-api > Settings > Instance type: pick the next size up."},
			{s.WebID, "Website server", "web", "Render > fcm-web > Settings > Instance type: pick the next size up."},
			{s.DBID, "Database", "db", "Render > fcm-db > Settings > Instance type: pick the next plan up (more memory and CPU)."},
		} {
			if t.id == "" {
				continue
			}
			mem, memLimit, err1 := render.peak(ctx, t.id, "memory", "memory-limit")
			cpu, cpuLimit, err2 := render.peak(ctx, t.id, "cpu", "cpu-limit")
			if err1 != nil || err2 != nil {
				e := err1
				if e == nil {
					e = err2
				}
				r.Render = "Render didn't answer: " + e.Error()
				continue
			}
			if memLimit > 0 {
				g := Gauge{Key: t.key + "_memory", Group: t.group, Label: "Memory (24 h peak)", Used: mem, Limit: memLimit, Unit: "bytes", Upgrade: t.upgrade, Peak: true}
				if t.key == "api" {
					apiMem = g
					continue
				}
				renderGauges = append(renderGauges, g)
			}
			if cpuLimit > 0 {
				renderGauges = append(renderGauges, Gauge{Key: t.key + "_cpu", Group: t.group, Label: "CPU (24 h peak hour)", Used: cpu, Limit: cpuLimit, Unit: "cpu", Upgrade: t.upgrade, Peak: true})
			}
		}
	}
	r.Gauges = append(r.Gauges, apiMem)
	r.Gauges = append(r.Gauges, renderGauges...)
	for i := range r.Gauges {
		r.Gauges[i].fill()
	}

	// ---- growth ----
	hist, _ := s.History(ctx, 60)
	r.Snapshot = hist
	if len(hist) >= 2 {
		first, last := hist[0], hist[len(hist)-1]
		days := last.At.Sub(first.At).Hours() / 24
		if days >= 1 {
			g := &Growth{Days: days,
				DBBytesPerDay:  float64(last.DBBytes-first.DBBytes) / days,
				PhotoBytesDay:  float64(last.PhotoBytes-first.PhotoBytes) / days,
				CardsPerDay:    float64(last.Cards-first.Cards) / days,
				AccountsPerDay: float64(last.Accounts-first.Accounts) / days}
			if g.DBBytesPerDay > 0 {
				left := (disk*1e9 - float64(dbBytes)) / g.DBBytesPerDay
				g.DBFullInDays = &left
			}
			r.Growth = g
		}
	}
	return r, nil
}

// Snap records today's snapshot (once a day; run it from the hourly tidy-up).
func (s *Service) Snap(ctx context.Context) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM resource_snapshots WHERE taken_at > now() - interval '20 hours'`).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO resource_snapshots (db_bytes, photo_bytes, photos, cards, accounts)
		SELECT pg_database_size(current_database()),
			(SELECT coalesce(sum(bytes), 0) FROM sync_photos WHERE status = 'ready'),
			(SELECT count(*) FROM sync_photos WHERE status = 'ready'),
			(SELECT count(*) FROM sync_cards WHERE deleted_at IS NULL),
			(SELECT count(*) FROM accounts)`)
	return err
}

func (s *Service) History(ctx context.Context, days int) ([]Snapshot, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT taken_at, db_bytes, photo_bytes, photos, cards, accounts FROM resource_snapshots
		WHERE taken_at > now() - make_interval(days => $1) ORDER BY taken_at`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var x Snapshot
		if err := rows.Scan(&x.At, &x.DBBytes, &x.PhotoBytes, &x.Photos, &x.Cards, &x.Accounts); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// High lists the gauges at 85% or more (for the back office Alerts and the daily email).
func (r Report) High() []Gauge {
	var out []Gauge
	for _, g := range r.Gauges {
		if g.Level == "high" {
			out = append(out, g)
		}
	}
	return out
}

func (s *Service) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (s *Service) base() string {
	if s.renderURL != "" {
		return s.renderURL
	}
	return "https://api.render.com/v1"
}

// SetRenderURL points the Render calls elsewhere (tests).
func (s *Service) SetRenderURL(u string) { s.renderURL = u }

// cgroupMemory is the container's memory use and limit (Linux cgroup v2, then v1).
func cgroupMemory() (int64, int64, bool) {
	read := func(p string) (int64, bool) {
		b, err := os.ReadFile(p)
		if err != nil {
			return 0, false
		}
		v := strings.TrimSpace(string(b))
		if v == "max" {
			return 0, false
		}
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	}
	if used, ok := read("/sys/fs/cgroup/memory.current"); ok {
		if limit, ok := read("/sys/fs/cgroup/memory.max"); ok && limit > 0 && limit < 1<<50 {
			return used, limit, true
		}
	}
	if used, ok := read("/sys/fs/cgroup/memory/memory.usage_in_bytes"); ok {
		if limit, ok := read("/sys/fs/cgroup/memory/memory.limit_in_bytes"); ok && limit > 0 && limit < 1<<50 {
			return used, limit, true
		}
	}
	return 0, 0, false
}

// ---------------- Render's API ----------------

type renderClient struct {
	key, base string
	http      *http.Client
}

func (c *renderClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("the Render API key was refused")
		}
		return fmt.Errorf("Render error %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

type series struct {
	Values []struct {
		Value float64 `json:"value"`
	} `json:"values"`
}

// metric reads one Render metric for the last 24 hours, hourly. Render
// answers with a list of series (one per instance).
func (c *renderClient) metric(ctx context.Context, resource, name string) ([]series, error) {
	now := time.Now().UTC()
	q := url.Values{"resource": {resource}, "startTime": {now.Add(-24 * time.Hour).Format(time.RFC3339)},
		"endTime": {now.Format(time.RFC3339)}, "resolutionSeconds": {"3600"}}
	var raw json.RawMessage
	if err := c.get(ctx, "/metrics/"+name, q, &raw); err != nil {
		return nil, err
	}
	var list []series
	if err := json.Unmarshal(raw, &list); err != nil {
		var wrapped struct {
			Data []series `json:"data"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil, fmt.Errorf("unexpected Render answer")
		}
		list = wrapped.Data
	}
	return list, nil
}

// peak is the highest value of a metric and the latest limit over the last day.
func (c *renderClient) peak(ctx context.Context, resource, usage, limit string) (float64, float64, error) {
	u, err := c.metric(ctx, resource, usage)
	if err != nil {
		return 0, 0, err
	}
	l, err := c.metric(ctx, resource, limit)
	if err != nil {
		return 0, 0, err
	}
	var hi, lim float64
	for _, s := range u {
		for _, v := range s.Values {
			if v.Value > hi {
				hi = v.Value
			}
		}
	}
	for _, s := range l {
		if n := len(s.Values); n > 0 && s.Values[n-1].Value > 0 {
			lim = s.Values[n-1].Value
		}
	}
	return hi, lim, nil
}

// diskGB is the database's disk size.
func (c *renderClient) diskGB(ctx context.Context, postgresID string) (float64, error) {
	var out struct {
		DiskSizeGB float64 `json:"diskSizeGB"`
	}
	err := c.get(ctx, "/postgres/"+url.PathEscape(postgresID), nil, &out)
	return out.DiskSizeGB, err
}
