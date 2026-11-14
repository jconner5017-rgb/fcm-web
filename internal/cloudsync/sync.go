// Package cloudsync keeps a seller's desktop inventory and the website in step.
//
// A device pushes the rows it changed, each with the version it last saw,
// then pulls every change after the last change number it has seen. The
// server numbers every accepted change, so devices never need to compare
// clocks to know what's new.
//
// Rows (boxes, cards, Sold Log entries) are stored as JSON objects of the
// desktop's fields. For each field the server remembers the version and
// time it last changed. When two devices edit the same card between syncs,
// fields only one side changed are both kept; if both changed the same
// field, the newer edit wins and the other value goes to the conflict log.
//
// Rules the server enforces for every device:
//   - Sold is final: a card whose status is Sold can't be made available again.
//   - Deletes win: a deleted row stays deleted, whatever edits arrive later.
//   - At most CardLimit cards in inventory (sold cards don't count).
package cloudsync

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	CardLimit     = 10_000
	MaxPushBatch  = 200
	MaxPullBatch  = 500
	maxFields     = 150
	maxFieldBytes = 64 << 10 // one field's JSON (a long HTML description fits)
	soldStatus    = "Sold"
)

var (
	ErrBatchTooBig = fmt.Errorf("send at most %d changes at a time", MaxPushBatch)
	fieldNameRE    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	uuidRE         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// table for each entity the desktop syncs.
var tables = map[string]string{"box": "sync_boxes", "card": "sync_cards", "sold": "sync_sold"}

// Change is one row a device changed since its last sync.
type Change struct {
	Entity      string                     `json:"entity"` // box, card or sold
	ID          string                     `json:"id"`     // permanent id (UUID) made when the row was first synced
	BaseVersion int                        `json:"base_version"`
	Deleted     bool                       `json:"deleted,omitempty"`
	Fields      map[string]json.RawMessage `json:"fields,omitempty"` // only the changed fields (all of them for a new row)
	EditedAt    time.Time                  `json:"edited_at"`        // when the edit was made on the device
}

// Result tells the device what happened to one change.
type Result struct {
	Entity  string   `json:"entity"`
	ID      string   `json:"id"`
	Status  string   `json:"status"` // applied, merged, unchanged, deleted, rejected
	Version int      `json:"version"`
	Reason  string   `json:"reason,omitempty"`
	Lost    []string `json:"lost_fields,omitempty"` // fields where a newer edit from elsewhere was kept
}

// Device is who is pushing or pulling.
type Device struct {
	ID        string
	AccountID string
	Edition   string
}

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

type fieldMeta struct {
	V  int       `json:"v"`
	At time.Time `json:"at"`
}

// Push applies a batch of changes. Each change is its own transaction, so
// one bad row never blocks the rest.
func (s *Service) Push(ctx context.Context, d Device, changes []Change) ([]Result, error) {
	if len(changes) > MaxPushBatch {
		return nil, ErrBatchTooBig
	}
	out := make([]Result, 0, len(changes))
	for _, c := range changes {
		r, err := s.apply(ctx, d, c)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func reject(c Change, why string) Result {
	return Result{Entity: c.Entity, ID: c.ID, Status: "rejected", Reason: why}
}

func validate(c Change) string {
	if _, ok := tables[c.Entity]; !ok {
		return "unknown entity"
	}
	if !uuidRE.MatchString(c.ID) {
		return "id must be a UUID"
	}
	if c.BaseVersion < 0 {
		return "bad base_version"
	}
	if len(c.Fields) > maxFields {
		return "too many fields"
	}
	for k, v := range c.Fields {
		if !fieldNameRE.MatchString(k) {
			return "bad field name: " + k
		}
		if len(v) > maxFieldBytes {
			return "field too large: " + k
		}
		if !json.Valid(v) {
			return "bad value for " + k
		}
	}
	return ""
}

// str reads a JSON string field ("" when absent or not a string).
func str(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

func (s *Service) apply(ctx context.Context, d Device, c Change) (Result, error) {
	c.ID = strings.ToLower(c.ID)
	if why := validate(c); why != "" {
		return reject(c, why), nil
	}
	editedAt := c.EditedAt
	if editedAt.IsZero() || editedAt.After(s.now().Add(5*time.Minute)) {
		editedAt = s.now() // a missing or future clock can't win every conflict
	}
	table := tables[c.Entity]

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()

	var (
		accountID, edition string
		version            int
		dataRaw, metaRaw   []byte
		deletedAt          sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `SELECT account_id, edition, version, data, field_meta, deleted_at FROM `+table+`
		WHERE id = $1 FOR UPDATE`, c.ID).Scan(&accountID, &edition, &version, &dataRaw, &metaRaw, &deletedAt)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if exists && (accountID != d.AccountID || edition != d.Edition) {
		return reject(c, "not found"), nil // another account's id: don't reveal it exists
	}

	res := Result{Entity: c.Entity, ID: c.ID}

	// ---- A row the server hasn't seen ----
	if !exists {
		if c.Deleted {
			res.Status = "unchanged" // made and deleted before it ever synced
			return res, nil
		}
		if c.Entity == "card" && str(c.Fields["status"]) != soldStatus {
			n, err := countInventory(ctx, tx, d.AccountID)
			if err != nil {
				return Result{}, err
			}
			if n >= CardLimit {
				return reject(c, "card_limit"), nil
			}
		}
		meta := map[string]fieldMeta{}
		for k := range c.Fields {
			meta[k] = fieldMeta{V: 1, At: editedAt}
		}
		if err := s.write(ctx, tx, c.Entity, d, c.ID, 1, c.Fields, meta, nil, true); err != nil {
			return Result{}, err
		}
		res.Status, res.Version = "applied", 1
		return res, tx.Commit()
	}

	// ---- Deletes win ----
	if deletedAt.Valid {
		res.Status, res.Version = "deleted", version
		return res, nil
	}

	data := map[string]json.RawMessage{}
	meta := map[string]fieldMeta{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return Result{}, err
	}

	if c.Deleted {
		now := s.now()
		if err := s.write(ctx, tx, c.Entity, d, c.ID, version+1, data, meta, &now, false); err != nil {
			return Result{}, err
		}
		res.Status, res.Version = "deleted", version+1
		return res, tx.Commit()
	}

	soldNow := c.Entity == "card" && str(data["status"]) == soldStatus
	changed, merged := false, false
	newVersion := version + 1
	for k, v := range c.Fields {
		old, had := data[k]
		if had && bytes.Equal(old, v) {
			continue
		}
		if k == "status" && soldNow && str(v) != soldStatus {
			res.Lost = append(res.Lost, k) // sold is final
			merged = true
			continue
		}
		m, seen := meta[k]
		if seen && m.V > c.BaseVersion {
			// This field also changed elsewhere since the device last saw the row.
			merged = true
			if !editedAt.After(m.At) {
				res.Lost = append(res.Lost, k)
				if quietField[k] {
					continue
				}
				if err := logConflict(ctx, tx, d, c, k, old, v, editedAt, d.ID); err != nil {
					return Result{}, err
				}
				continue
			}
			if !quietField[k] {
				if err := logConflict(ctx, tx, d, c, k, v, old, m.At, ""); err != nil {
					return Result{}, err
				}
			}
		}
		data[k] = v
		meta[k] = fieldMeta{V: newVersion, At: editedAt}
		changed = true
	}
	if !changed {
		res.Status, res.Version = "unchanged", version
		if merged {
			res.Status = "merged"
		}
		return res, tx.Commit()
	}
	// A card coming back from Sold is blocked above; one going TO the
	// inventory from a deleted/unsold state can't exceed the limit either,
	// but an existing unsold card is already counted, so nothing to check.
	if err := s.write(ctx, tx, c.Entity, d, c.ID, newVersion, data, meta, nil, false); err != nil {
		return Result{}, err
	}
	res.Status, res.Version = "applied", newVersion
	if merged {
		res.Status = "merged"
	}
	return res, tx.Commit()
}

func countInventory(ctx context.Context, tx *sql.Tx, accountID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sync_cards
		WHERE account_id = $1 AND deleted_at IS NULL AND coalesce(status, '') <> $2`, accountID, soldStatus).Scan(&n)
	return n, err
}

// quietField are bookkeeping timestamps every save touches: the newer one
// still wins, but there's nothing for the seller to choose, so no conflict.
var quietField = map[string]bool{"updated_at": true, "created_at": true, "last_priced_at": true, "cardsight_checked_at": true}

// logConflict records an edit that lost. lostDevice is "" when the losing
// edit was the one already on the server (its device isn't tracked per field).
func logConflict(ctx context.Context, tx *sql.Tx, d Device, c Change, field string, kept, lost json.RawMessage, lostAt time.Time, lostDevice string) error {
	keptV, lostV := any(nil), any(nil)
	if kept != nil {
		keptV = string(kept)
	}
	if lost != nil {
		lostV = string(lost)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_conflicts (account_id, entity, entity_id, field, kept_value, lost_value, lost_device, lost_edited_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)`, d.AccountID, c.Entity, c.ID, field, keptV, lostV, nullID(lostDevice), lostAt)
	return err
}

func nullID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// write stores a row's new state and adds it to the change feed.
func (s *Service) write(ctx context.Context, tx *sql.Tx, entity string, d Device, id string, version int,
	data map[string]json.RawMessage, meta map[string]fieldMeta, deletedAt *time.Time, insert bool) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	now := s.now()
	table := tables[entity]
	switch {
	case insert && entity == "card":
		_, err = tx.ExecContext(ctx, `INSERT INTO sync_cards (id, account_id, edition, version, data, field_meta, box_id, status, updated_at, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)`,
			id, d.AccountID, d.Edition, version, dataJSON, metaJSON, uuidOrNil(str(data["box_id"])), str(data["status"]), now, nullID(d.ID))
	case insert:
		_, err = tx.ExecContext(ctx, `INSERT INTO `+table+` (id, account_id, edition, version, data, field_meta, updated_at, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, d.AccountID, d.Edition, version, dataJSON, metaJSON, now, nullID(d.ID))
	case entity == "card":
		_, err = tx.ExecContext(ctx, `UPDATE sync_cards SET version = $2, data = $3, field_meta = $4, box_id = $5,
			status = NULLIF($6, ''), deleted_at = $7, updated_at = $8, updated_by = $9 WHERE id = $1`,
			id, version, dataJSON, metaJSON, uuidOrNil(str(data["box_id"])), str(data["status"]), deletedAt, now, nullID(d.ID))
	default:
		_, err = tx.ExecContext(ctx, `UPDATE `+table+` SET version = $2, data = $3, field_meta = $4, deleted_at = $5,
			updated_at = $6, updated_by = $7 WHERE id = $1`, id, version, dataJSON, metaJSON, deletedAt, now, nullID(d.ID))
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_changes (account_id, entity, entity_id, version, device_id, changed_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, d.AccountID, entity, id, version, nullID(d.ID), now)
	return err
}

func uuidOrNil(s string) any {
	if uuidRE.MatchString(s) {
		return strings.ToLower(s)
	}
	return nil
}

// Pulled is one row's current state, as the device should store it.
type Pulled struct {
	Seq     int64                      `json:"seq"`
	Entity  string                     `json:"entity"`
	ID      string                     `json:"id"`
	Version int                        `json:"version"`
	Deleted bool                       `json:"deleted,omitempty"`
	Fields  map[string]json.RawMessage `json:"fields,omitempty"`
	// FromDevice is the device that made the change ("" for the website).
	// A device can skip a row it sent itself when the version matches.
	FromDevice string `json:"from_device,omitempty"`
}

// PullResult is one page of the change feed.
type PullResult struct {
	Changes []Pulled `json:"changes"`
	Next    int64    `json:"next"` // pass as `after` next time
	More    bool     `json:"more"`
}

// Pull returns changes after seq `after` for the device's edition. Each row
// appears once per page, in its latest state.
func (s *Service) Pull(ctx context.Context, d Device, after int64, limit int) (PullResult, error) {
	if limit <= 0 || limit > MaxPullBatch {
		limit = MaxPullBatch
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT seq, entity, entity_id, coalesce(device_id::text, '') FROM sync_changes
		WHERE account_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`, d.AccountID, after, limit+1)
	if err != nil {
		return PullResult{}, err
	}
	type ref struct {
		seq            int64
		entity, id, by string
	}
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.seq, &r.entity, &r.id, &r.by); err != nil {
			rows.Close()
			return PullResult{}, err
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PullResult{}, err
	}
	res := PullResult{Changes: []Pulled{}, Next: after}
	if len(refs) > limit {
		res.More = true
		refs = refs[:limit]
	}
	if len(refs) == 0 {
		return res, nil
	}
	res.Next = refs[len(refs)-1].seq

	// Latest seq per row on this page.
	last := map[string]int{}
	ids := map[string][]string{}
	for i, r := range refs {
		k := r.entity + ":" + r.id
		if _, ok := last[k]; !ok {
			ids[r.entity] = append(ids[r.entity], r.id)
		}
		last[k] = i
	}
	state := map[string]Pulled{}
	for entity, list := range ids {
		q, err := s.DB.QueryContext(ctx, `SELECT id, edition, version, data, deleted_at IS NOT NULL FROM `+tables[entity]+`
			WHERE account_id = $1 AND id = ANY($2::uuid[])`, d.AccountID, pq.Array(list))
		if err != nil {
			return PullResult{}, err
		}
		for q.Next() {
			var p Pulled
			var edition string
			var data []byte
			if err := q.Scan(&p.ID, &edition, &p.Version, &data, &p.Deleted); err != nil {
				q.Close()
				return PullResult{}, err
			}
			if edition != d.Edition {
				continue // the other app's rows
			}
			p.Entity = entity
			if !p.Deleted {
				if err := json.Unmarshal(data, &p.Fields); err != nil {
					q.Close()
					return PullResult{}, err
				}
			}
			state[entity+":"+p.ID] = p
		}
		q.Close()
	}
	for i, r := range refs {
		k := r.entity + ":" + r.id
		if last[k] != i {
			continue
		}
		p, ok := state[k]
		if !ok {
			continue
		}
		p.Seq, p.FromDevice = r.seq, r.by
		res.Changes = append(res.Changes, p)
	}
	return res, nil
}

// Status is the sync summary the desktop shows on its Cloud Sync page.
type Status struct {
	LatestSeq     int64 `json:"latest_seq"`
	Cards         int   `json:"cards"` // in inventory, this edition
	AllCards      int   `json:"all_cards"`
	CardLimit     int   `json:"card_limit"`
	Boxes         int   `json:"boxes"`
	Sold          int   `json:"sold"`
	OpenConflicts int   `json:"open_conflicts"`
}

func (s *Service) Status(ctx context.Context, d Device) (Status, error) {
	st := Status{CardLimit: CardLimit}
	err := s.DB.QueryRowContext(ctx, `SELECT
			coalesce((SELECT max(seq) FROM sync_changes WHERE account_id = $1), 0),
			(SELECT count(*) FROM sync_cards WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL AND coalesce(status, '') <> $3),
			(SELECT count(*) FROM sync_cards WHERE account_id = $1 AND deleted_at IS NULL AND coalesce(status, '') <> $3),
			(SELECT count(*) FROM sync_boxes WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL),
			(SELECT count(*) FROM sync_sold WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL),
			(SELECT count(*) FROM sync_conflicts WHERE account_id = $1 AND resolved_at IS NULL)`,
		d.AccountID, d.Edition, soldStatus).Scan(&st.LatestSeq, &st.Cards, &st.AllCards, &st.Boxes, &st.Sold, &st.OpenConflicts)
	return st, err
}

// ServerSet changes fields on a row on the server's own behalf (a photo
// finished uploading, say) and adds it to the change feed so every device
// pulls the new values. device is "" when no device made the change.
func (s *Service) ServerSet(ctx context.Context, tx *sql.Tx, accountID, entity, id, device string, fields map[string]json.RawMessage) (int, error) {
	table, ok := tables[entity]
	if !ok {
		return 0, errors.New("unknown entity")
	}
	var edition string
	var version int
	var dataRaw, metaRaw []byte
	var deletedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT edition, version, data, field_meta, deleted_at FROM `+table+`
		WHERE id = $1 AND account_id = $2 FOR UPDATE`, id, accountID).Scan(&edition, &version, &dataRaw, &metaRaw, &deletedAt)
	if err != nil {
		return 0, err
	}
	if deletedAt.Valid {
		return version, nil
	}
	data := map[string]json.RawMessage{}
	meta := map[string]fieldMeta{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return 0, err
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return 0, err
	}
	version++
	for k, v := range fields {
		data[k] = v
		meta[k] = fieldMeta{V: version, At: s.now()}
	}
	d := Device{ID: device, AccountID: accountID, Edition: edition}
	return version, s.write(ctx, tx, entity, d, id, version, data, meta, nil, false)
}
