// Package lister is the website's Bulk Lister: it lists a seller's synced
// cards on a marketplace (Slabs & Singles for now) with the seller's own key,
// in the background, and records each listing on the card through the sync
// engine so FireCardManager on the PC shows it as live.
package lister

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/ebay"
	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
	"github.com/jconner5017-rgb/fcm-web/internal/templates"
	"github.com/lib/pq"
)

const (
	MaxPerJob  = 500
	keyPurpose = "market-key:"
)

var (
	ErrNoKey   = errors.New("no marketplace key")
	ErrNoCards = errors.New("no cards")
)

type Sealer interface {
	SealFor(purpose string, secret []byte) ([]byte, error)
	OpenFor(purpose string, box []byte) ([]byte, error)
}

type PhotoReader interface {
	Read(ctx context.Context, accountID, photoID string) ([]byte, error)
}

type Service struct {
	DB     *sql.DB
	Sync   *cloudsync.Service
	Sealer Sealer
	Photos PhotoReader
	Slabs  *slabs.Client
	Ebay   *ebay.Service
	// PhotoLink is a card photo's hosted link (what eBay copies the photos from).
	PhotoLink func(photoID string) string
	Log       *slog.Logger
	polCache  sync.Map // eBay policies per account and shipping profile
	Templates *templates.Service
	Pause     time.Duration // between cards (be gentle with the marketplace)
}

// ---------------- keys ----------------

type KeyInfo struct {
	Market     string          `json:"market"`
	Last4      string          `json:"last4"`
	Source     string          `json:"source"`
	DeviceName string          `json:"device_name,omitempty"`
	Settings   json.RawMessage `json:"settings,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// SaveKey stores a marketplace key. From a PC, a key typed on the website is kept.
func (s *Service) SaveKey(ctx context.Context, accountID, market, key string, settings json.RawMessage, source, deviceID string) error {
	key = strings.TrimSpace(key)
	if market != "slabs" {
		return fmt.Errorf("unknown marketplace")
	}
	if len(key) < 16 || len(key) > 300 || strings.ContainsAny(key, " \t\r\n") {
		return errBadKey
	}
	if len(settings) == 0 || !json.Valid(settings) {
		settings = json.RawMessage("{}")
	}
	box, err := s.Sealer.SealFor(keyPurpose+market, []byte(key))
	if err != nil {
		return err
	}
	q := `INSERT INTO account_market_keys (account_id, market, key_enc, key_last4, settings, source, device_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (account_id, market) DO UPDATE SET key_enc = EXCLUDED.key_enc, key_last4 = EXCLUDED.key_last4,
			settings = EXCLUDED.settings, source = EXCLUDED.source, device_id = EXCLUDED.device_id, updated_at = now()`
	if source == "pc" {
		q += ` WHERE account_market_keys.source = 'pc'`
	}
	var dev any
	if deviceID != "" {
		dev = deviceID
	}
	_, err = s.DB.ExecContext(ctx, q, accountID, market, box, key[len(key)-4:], string(settings), source, dev)
	return err
}

var errBadKey = errors.New("that doesn't look like a Slabs & Singles API key")

// IsBadKey reports a key that failed the format check.
func IsBadKey(err error) bool { return errors.Is(err, errBadKey) }

func (s *Service) DeleteKey(ctx context.Context, accountID, market, source string) error {
	q := `DELETE FROM account_market_keys WHERE account_id = $1 AND market = $2`
	if source == "pc" {
		q += ` AND source = 'pc'`
	}
	_, err := s.DB.ExecContext(ctx, q, accountID, market)
	return err
}

func (s *Service) Keys(ctx context.Context, accountID string) ([]KeyInfo, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT k.market, k.key_last4, k.source, coalesce(d.name, ''), k.settings, k.updated_at
		FROM account_market_keys k LEFT JOIN devices d ON d.id = k.device_id WHERE k.account_id = $1 ORDER BY k.market`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KeyInfo{}
	for rows.Next() {
		var k KeyInfo
		var st []byte
		if err := rows.Scan(&k.Market, &k.Last4, &k.Source, &k.DeviceName, &st, &k.UpdatedAt); err != nil {
			return nil, err
		}
		k.Settings = st
		out = append(out, k)
	}
	return out, rows.Err()
}

// SaveSettings stores a linked PC's marketplace defaults (package, weights).
func (s *Service) SaveSettings(ctx context.Context, accountID, market string, settings json.RawMessage) error {
	if len(settings) == 0 || !json.Valid(settings) {
		settings = json.RawMessage("{}")
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO account_market_settings (account_id, market, settings, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (account_id, market) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()`, accountID, market, string(settings))
	return err
}

// key is the website's own key for the marketplace, with the PC's defaults.
// Key is the seller's marketplace key (for calls other than listing, like
// marking an order shipped).
func (s *Service) Key(ctx context.Context, accountID, market string) (string, error) {
	k, _, err := s.key(ctx, accountID, market)
	return k, err
}

func (s *Service) key(ctx context.Context, accountID, market string) (string, json.RawMessage, error) {
	var box, settings []byte
	err := s.DB.QueryRowContext(ctx, `SELECT k.key_enc, coalesce(m.settings, k.settings) FROM account_market_keys k
		LEFT JOIN account_market_settings m ON m.account_id = k.account_id AND m.market = k.market
		WHERE k.account_id = $1 AND k.market = $2`, accountID, market).Scan(&box, &settings)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNoKey
	}
	if err != nil {
		return "", nil, err
	}
	k, err := s.Sealer.OpenFor(keyPurpose+market, box)
	return string(k), settings, err
}

// ---------------- jobs ----------------

type Item struct {
	Position int        `json:"position"`
	CardID   string     `json:"card_id"`
	Name     string     `json:"name"`
	Status   string     `json:"status"` // pending, listed, updated, skipped, failed
	Message  string     `json:"message,omitempty"`
	DoneAt   *time.Time `json:"done_at,omitempty"`
}

type Job struct {
	ID         string          `json:"id"`
	Market     string          `json:"market"`
	Edition    string          `json:"edition"`
	Status     string          `json:"status"`
	Options    json.RawMessage `json:"options"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Counts     map[string]int  `json:"counts"`
	Items      []Item          `json:"items,omitempty"`
}

// Start queues a run over the given cards (the account's, in this edition,
// still in inventory); the worker picks it up within a couple of seconds.
func (s *Service) Start(ctx context.Context, accountID, edition, market, jobID string, cardIDs []string, opts slabs.Options) (Job, error) {
	switch market {
	case "slabs":
		if _, _, err := s.key(ctx, accountID, market); err != nil {
			return Job{}, err
		}
	case "ebay":
		if err := s.ebayReady(ctx, accountID); err != nil {
			return Job{}, err
		}
	default:
		return Job{}, fmt.Errorf("unknown marketplace")
	}
	if len(cardIDs) == 0 {
		return Job{}, ErrNoCards
	}
	if len(cardIDs) > MaxPerJob {
		cardIDs = cardIDs[:MaxPerJob]
	}
	optJSON, _ := json.Marshal(opts)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO list_jobs (id, account_id, edition, market, options) VALUES ($1, $2, $3, $4, $5)`,
		jobID, accountID, edition, market, string(optJSON)); err != nil {
		return Job{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO list_job_items (job_id, position, card_id, name)
		SELECT $1, row_number() OVER (ORDER BY array_position($4::uuid[], c.id)), c.id, coalesce(c.data->>'player_name', '')
		FROM sync_cards c
		WHERE c.account_id = $2 AND c.edition = $3 AND c.id = ANY($4::uuid[]) AND c.deleted_at IS NULL AND coalesce(c.status, '') <> 'Sold'`,
		jobID, accountID, edition, pq.Array(cardIDs))
	if err != nil {
		return Job{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Job{}, ErrNoCards
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.Get(ctx, accountID, jobID)
}

func (s *Service) Get(ctx context.Context, accountID, jobID string) (Job, error) {
	var j Job
	var opts []byte
	var fin sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT id, market, edition, status, options, created_at, finished_at FROM list_jobs
		WHERE id::text = $1 AND account_id = $2`, strings.ToLower(jobID), accountID).Scan(&j.ID, &j.Market, &j.Edition, &j.Status, &opts, &j.CreatedAt, &fin)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, sql.ErrNoRows
	}
	if err != nil {
		return Job{}, err
	}
	j.Options = opts
	if fin.Valid {
		j.FinishedAt = &fin.Time
	}
	j.Counts = map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT position, card_id, coalesce(name, ''), status, coalesce(message, ''), done_at
		FROM list_job_items WHERE job_id = $1 ORDER BY position`, j.ID)
	if err != nil {
		return j, err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		var done sql.NullTime
		if err := rows.Scan(&it.Position, &it.CardID, &it.Name, &it.Status, &it.Message, &done); err != nil {
			return j, err
		}
		if done.Valid {
			it.DoneAt = &done.Time
		}
		j.Counts[it.Status]++
		j.Items = append(j.Items, it)
	}
	return j, rows.Err()
}

// Recent lists the account's last runs (without their items).
func (s *Service) Recent(ctx context.Context, accountID string) ([]Job, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT j.id, j.market, j.edition, j.status, j.created_at, j.finished_at,
			count(*) FILTER (WHERE i.status IN ('listed', 'updated')), count(*) FILTER (WHERE i.status IN ('skipped', 'failed')),
			count(*) FILTER (WHERE i.status = 'pending')
		FROM list_jobs j JOIN list_job_items i ON i.job_id = j.id
		WHERE j.account_id = $1 GROUP BY j.id ORDER BY j.created_at DESC LIMIT 10`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		var fin sql.NullTime
		var ok, bad, pending int
		if err := rows.Scan(&j.ID, &j.Market, &j.Edition, &j.Status, &j.CreatedAt, &fin, &ok, &bad, &pending); err != nil {
			return nil, err
		}
		if fin.Valid {
			j.FinishedAt = &fin.Time
		}
		j.Counts = map[string]int{"done": ok, "problems": bad, "pending": pending}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Stop ends a run; cards not reached yet are skipped.
func (s *Service) Stop(ctx context.Context, accountID, jobID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE list_jobs SET status = 'stopped', finished_at = now()
		WHERE id::text = $1 AND account_id = $2 AND status = 'running'`, strings.ToLower(jobID), accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, err = s.DB.ExecContext(ctx, `UPDATE list_job_items SET status = 'skipped', message = 'Stopped before this card.', done_at = now()
			WHERE job_id::text = $1 AND status = 'pending'`, strings.ToLower(jobID))
	}
	return err
}

// ---------------- the worker ----------------

// RunOnce lists the next waiting card of any running job. Returns false when
// there was nothing to do.
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	var jobID, accountID, edition, market, cardID string
	var position int
	var opts []byte
	err := s.DB.QueryRowContext(ctx, `
		SELECT i.job_id, i.position, i.card_id, j.account_id, j.edition, j.market, j.options
		FROM list_job_items i JOIN list_jobs j ON j.id = i.job_id
		WHERE i.status = 'pending' AND j.status = 'running'
		ORDER BY j.created_at, i.position LIMIT 1`).Scan(&jobID, &position, &cardID, &accountID, &edition, &market, &opts)
	if errors.Is(err, sql.ErrNoRows) {
		// Close finished runs.
		_, err = s.DB.ExecContext(ctx, `UPDATE list_jobs j SET status = 'done', finished_at = now()
			WHERE status = 'running' AND NOT EXISTS (SELECT 1 FROM list_job_items i WHERE i.job_id = j.id AND i.status = 'pending')`)
		return false, err
	}
	if err != nil {
		return false, err
	}
	var o slabs.Options
	_ = json.Unmarshal(opts, &o)
	var status, msg string
	if market == "ebay" {
		status, msg = s.listEbay(ctx, accountID, edition, cardID, o)
	} else {
		status, msg = s.listOne(ctx, accountID, edition, cardID, o)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE list_job_items SET status = $3, message = NULLIF($4, ''), done_at = now()
		WHERE job_id = $1 AND position = $2`, jobID, position, status, msg)
	if s.Pause > 0 {
		time.Sleep(s.Pause)
	}
	return true, err
}

// Run works through queued cards until ctx ends.
func (s *Service) Run(ctx context.Context) {
	for {
		did, err := s.RunOnce(ctx)
		if err != nil && s.Log != nil && ctx.Err() == nil {
			s.Log.Warn("bulk lister", "err", err)
		}
		if !did {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (s *Service) listOne(ctx context.Context, accountID, edition, cardID string, o slabs.Options) (string, string) {
	key, settings, err := s.key(ctx, accountID, "slabs")
	if err != nil {
		return "failed", "Add your Slabs & Singles API key on the Account page."
	}
	var ship slabs.Ship
	_ = json.Unmarshal(settings, &ship)

	var raw []byte
	var version int
	var deleted bool
	err = s.DB.QueryRowContext(ctx, `SELECT data, version, deleted_at IS NOT NULL FROM sync_cards WHERE id = $1 AND account_id = $2`,
		cardID, accountID).Scan(&raw, &version, &deleted)
	if err != nil || deleted {
		return "skipped", "This card is no longer in your inventory."
	}
	var c slabs.Card
	_ = json.Unmarshal(raw, &c)
	var data map[string]json.RawMessage
	_ = json.Unmarshal(raw, &data)
	if c.Status == "Sold" {
		return "skipped", "Already sold."
	}
	if c.Sku == "" {
		return "skipped", "This card hasn't fully synced from your PC yet (no SKU)."
	}
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(data[k], &v)
		return v
	}

	// Photos: reuse what this card already has on the marketplace, else upload front and back.
	var photos []string
	if prev := str("marketplace_photo_urls"); prev != "" {
		photos = strings.Split(prev, "|")
	}
	if len(photos) == 0 && str("front_photo_id") != "" {
		if p := slabs.Problems(c, edition, true, o); len(p) > 0 {
			return "skipped", "Not listed: " + strings.Join(p, ", ") + "."
		}
		for _, side := range []string{"front_photo_id", "back_photo_id"} {
			id := str(side)
			if id == "" {
				continue
			}
			jpeg, err := s.Photos.Read(ctx, accountID, id)
			if err != nil {
				continue
			}
			url, err := s.Slabs.UploadPhoto(ctx, key, jpeg)
			if err != nil {
				return "failed", describe(err)
			}
			photos = append(photos, url)
		}
	}
	if p := slabs.Problems(c, edition, len(photos) > 0, o); len(p) > 0 {
		return "skipped", "Not listed: " + strings.Join(p, ", ") + "."
	}
	in := slabs.Build(c, edition, photos, o, ship)
	listing, created, err := s.Slabs.Upsert(ctx, key, in, c.Sku+"-"+fmt.Sprint(time.Now().UnixNano()))
	if err != nil {
		return "failed", describe(err)
	}

	// Record it on the card; the PC pulls it and shows the card live on S&S.
	idJSON, _ := json.Marshal(listing.ID)
	urlsJSON, _ := json.Marshal(strings.Join(photos, "|"))
	now := time.Now()
	at, _ := json.Marshal(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
	res, err := s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
		Entity: "card", ID: cardID, BaseVersion: version, EditedAt: now,
		Fields: map[string]json.RawMessage{"marketplace_listing_id": idJSON, "marketplace_listed_at": at, "marketplace_photo_urls": urlsJSON, "updated_at": at},
	}})
	if err != nil || len(res) == 0 || res[0].Status == "rejected" {
		return "failed", "Listed on Slabs & Singles, but the card couldn't be updated here. It shows as live after your PC's next Slabs & Singles check."
	}
	if created {
		return "listed", "Listed."
	}
	return "updated", "Listing updated."
}

func describe(err error) string {
	var api *slabs.APIError
	if errors.As(err, &api) {
		switch api.Status {
		case 401:
			return "Slabs & Singles didn't accept your API key. Check it on the Account page."
		case 403:
			return "Slabs & Singles: " + orDefault(api.Message, "your seller account can't list right now.")
		}
		return "Slabs & Singles: " + orDefault(api.Message, fmt.Sprintf("error %d", api.Status))
	}
	return err.Error()
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
