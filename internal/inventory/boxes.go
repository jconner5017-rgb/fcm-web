package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
)

// Box sizes and types, the same rules as FireCardManager's BoxRules
// (2026-11-15). Every box has a size in cards (data "capacity"; boxes from
// before have none and hold the old 1,300 = rows A-Z) and a type: raw cards
// (toploaders too), memorabilia/relic/oversized (Sports only) or slabs, each
// in its own box per category ("Baseball", "Baseball Relics", "Baseball
// Slabs"). Rows stay 50 cards; a closed box ("closed_at") takes no new
// cards. New cards go after the last card, never into slots sold cards left,
// unless the seller turned on "fill sold slots".

const (
	KindRaw   = "raw"
	KindRelic = "relic"
	KindSlab  = "slab"

	LegacyCapacity = SlotsPerRow * MaxRowsPerBox // 1,300
	MinCapacity    = 5
	MaxCapacity    = 10000
)

// NormalizeKind is raw, relic (Sports only) or slab.
func NormalizeKind(kind, edition string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindSlab:
		return KindSlab
	case KindRelic:
		if edition != "tcg" {
			return KindRelic
		}
	}
	return KindRaw
}

func kindSuffix(kind string) string {
	switch kind {
	case KindRelic:
		return " Relics"
	case KindSlab:
		return " Slabs"
	}
	return ""
}

// BoxBaseName is "Baseball", "Baseball Relics" or "Baseball Slabs".
func BoxBaseName(category, kind string) string { return category + kindSuffix(kind) }

// KindOfBoxName reads the type from a box's name.
func KindOfBoxName(name string) string {
	b := strings.ToLower(strings.TrimSpace(strings.SplitN(name, " (cont. ", 2)[0]))
	switch {
	case strings.HasSuffix(b, " slabs"):
		return KindSlab
	case strings.HasSuffix(b, " relics"):
		return KindRelic
	}
	return KindRaw
}

// Position is a slot's 1-based place in its box: A-01 is 1, B-01 is 51.
func Position(row string, slot int) int { return RowIndex(row)*SlotsPerRow + slot }

// SlotAt is Position's inverse.
func SlotAt(p int) (string, int) { return RowLetter((p - 1) / SlotsPerRow), (p-1)%SlotsPerRow + 1 }

// RowsFor is how many rows of 50 a box of this size has.
func RowsFor(capacity int) int { return (capacity + SlotsPerRow - 1) / SlotsPerRow }

// BoxSizes are the seller's sizes for new boxes of each type.
type BoxSizes struct {
	Raw      *int `json:"raw"`
	Relic    *int `json:"relic"`
	Slab     *int `json:"slab"`
	FillSold bool `json:"fill_sold"`
	Chosen   bool `json:"chosen"`
}

func (b BoxSizes) For(kind string) *int {
	switch kind {
	case KindRelic:
		return b.Relic
	case KindSlab:
		return b.Slab
	}
	return b.Raw
}

func (s *Service) BoxSizes(ctx context.Context, accountID, edition string) (BoxSizes, error) {
	var out BoxSizes
	var raw, relic, slab sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT raw_size, relic_size, slab_size, fill_sold FROM account_box_sizes WHERE account_id = $1 AND edition = $2`,
		accountID, edition).Scan(&raw, &relic, &slab, &out.FillSold)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	ip := func(v sql.NullInt64) *int {
		if !v.Valid {
			return nil
		}
		n := int(v.Int64)
		return &n
	}
	out.Raw, out.Relic, out.Slab, out.Chosen = ip(raw), ip(relic), ip(slab), true
	return out, nil
}

func (s *Service) SaveBoxSizes(ctx context.Context, accountID, edition string, in BoxSizes) error {
	bad := FieldErrors{}
	for k, v := range map[string]*int{"raw": in.Raw, "relic": in.Relic, "slab": in.Slab} {
		if k == "relic" && edition == "tcg" {
			continue
		}
		if v == nil || *v < MinCapacity || *v > MaxCapacity {
			bad[k] = fmt.Sprintf("Enter a number of cards from %d to %d.", MinCapacity, MaxCapacity)
		}
	}
	if len(bad) > 0 {
		return bad
	}
	if edition == "tcg" {
		in.Relic = nil
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO account_box_sizes (account_id, edition, raw_size, relic_size, slab_size, fill_sold, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (account_id, edition) DO UPDATE SET raw_size = EXCLUDED.raw_size, relic_size = EXCLUDED.relic_size,
			slab_size = EXCLUDED.slab_size, fill_sold = EXCLUDED.fill_sold, updated_at = now()`,
		accountID, edition, in.Raw, in.Relic, in.Slab, in.FillSold)
	return err
}

// boxState is what filing needs to know about a box.
type boxState struct {
	ID, Name, Kind, Edition string
	Capacity                int
	HasCapacity, Closed     bool
	Version                 int
	HighWater               int // furthest slot ever used; new cards go after it
}

func (s *Service) box(ctx context.Context, accountID, boxID string) (boxState, error) {
	var b boxState
	var capText, kind, closed, high sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id, edition, version, coalesce(data->>'name', ''), data->>'capacity', data->>'kind', data->>'closed_at', data->>'high_water'
		FROM sync_boxes WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`, boxID, accountID).
		Scan(&b.ID, &b.Edition, &b.Version, &b.Name, &capText, &kind, &closed, &high)
	if err != nil {
		return b, err
	}
	b.Capacity = LegacyCapacity
	if n, err := fmt.Sscan(capText.String, &b.Capacity); n == 1 && err == nil && b.Capacity > 0 {
		b.HasCapacity = true
		if b.Capacity > MaxCapacity {
			b.Capacity = MaxCapacity
		}
	} else {
		b.Capacity = LegacyCapacity
	}
	b.Kind = kind.String
	if b.Kind == "" {
		b.Kind = KindOfBoxName(b.Name)
	}
	b.Closed = closed.Valid && closed.String != ""
	if high.Valid {
		_, _ = fmt.Sscan(high.String, &b.HighWater)
	}
	return b, nil
}

// positions are the places taken in a box (unsold cards with a row and slot).
func (s *Service) positions(ctx context.Context, accountID, boxID string) (map[int]bool, int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data->>'slot_row', (data->>'slot_number')::int
		FROM sync_cards WHERE account_id = $1 AND box_id = $2 AND deleted_at IS NULL AND coalesce(status, '') <> $3
		  AND coalesce(data->>'slot_row', '') <> '' AND jsonb_typeof(data->'slot_number') = 'number'`, accountID, boxID, soldStatus)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	taken, last := map[int]bool{}, 0
	for rows.Next() {
		var r string
		var n int
		if err := rows.Scan(&r, &n); err != nil {
			return nil, 0, err
		}
		if RowIndex(r) < 0 || n < 1 {
			continue
		}
		p := Position(r, n)
		taken[p] = true
		if p > last {
			last = p
		}
	}
	return taken, last, rows.Err()
}

// SetBox changes a box's size (up any time, down only to its last used
// slot) or closes / reopens it. Goes through sync, so the PC gets it.
func (s *Service) SetBox(ctx context.Context, accountID, boxID string, capacity *int, closed *bool) (BoxInfo, error) {
	unlock, err := s.lockFiling(ctx, accountID)
	if err != nil {
		return BoxInfo{}, err
	}
	defer unlock()
	b, err := s.box(ctx, accountID, boxID)
	if errors.Is(err, sql.ErrNoRows) {
		return BoxInfo{}, ErrNotFound
	}
	if err != nil {
		return BoxInfo{}, err
	}
	_, last, err := s.positions(ctx, accountID, boxID)
	if err != nil {
		return BoxInfo{}, err
	}
	fields := map[string]json.RawMessage{}
	if capacity != nil {
		switch {
		case *capacity < MinCapacity || *capacity > MaxCapacity:
			return BoxInfo{}, FieldErrors{"capacity": fmt.Sprintf("A box holds %d to %d cards.", MinCapacity, MaxCapacity)}
		case *capacity < last:
			r, n := SlotAt(last)
			return BoxInfo{}, FieldErrors{"capacity": fmt.Sprintf("This box has a card at row %s slot %02d, so it can't hold fewer than %d. Move cards out in FireCardManager to make it smaller.", r, n, last)}
		}
		fields["capacity"] = json.RawMessage(fmt.Sprint(*capacity))
		kindJSON, _ := json.Marshal(b.Kind)
		fields["kind"] = kindJSON
	}
	if closed != nil {
		if strings.EqualFold(strings.TrimSpace(b.Name), "unsorted") {
			return BoxInfo{}, FieldErrors{"closed": "The Unsorted box stays open."}
		}
		if *closed {
			fields["closed_at"] = timeJSON(time.Now())
		} else {
			fields["closed_at"] = json.RawMessage("null")
		}
	}
	if len(fields) > 0 {
		if _, err := s.push(ctx, cloudsync.Device{AccountID: accountID, Edition: b.Edition}, cloudsync.Change{
			Entity: "box", ID: boxID, BaseVersion: b.Version, Fields: fields, EditedAt: time.Now(),
		}); err != nil {
			return BoxInfo{}, err
		}
	}
	return s.BoxInfo(ctx, accountID, boxID)
}

// BoxInfo is a box's size, how far it's filled and whether it's closed.
type BoxInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Capacity int    `json:"capacity"`
	Older    bool   `json:"older,omitempty"` // made before box sizes (1,300)
	FilledTo int    `json:"filled_to"`       // place of the last card (A-01 = 1)
	LastRow  string `json:"last_row,omitempty"`
	LastSlot int    `json:"last_slot,omitempty"`
	Closed   bool   `json:"closed"`
}

func (s *Service) BoxInfo(ctx context.Context, accountID, boxID string) (BoxInfo, error) {
	b, err := s.box(ctx, accountID, boxID)
	if errors.Is(err, sql.ErrNoRows) {
		return BoxInfo{}, ErrNotFound
	}
	if err != nil {
		return BoxInfo{}, err
	}
	_, last, err := s.positions(ctx, accountID, boxID)
	if err != nil {
		return BoxInfo{}, err
	}
	out := BoxInfo{ID: b.ID, Name: b.Name, Kind: b.Kind, Capacity: b.Capacity, Older: !b.HasCapacity, FilledTo: last, Closed: b.Closed}
	if last > 0 {
		out.LastRow, out.LastSlot = SlotAt(last)
	}
	if b.HighWater > out.FilledTo {
		out.FilledTo = b.HighWater
	}
	return out, nil
}
