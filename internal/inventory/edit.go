package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
)

// Edits from the website go through the same sync engine as the desktop's
// (as a change from "the website", device id ""), so the field merge, the
// conflict log and "sold is final" work the same, and every linked PC pulls
// the edit on its next sync.

var (
	ErrSoldAlready = errors.New("this card is already sold")
	ErrLive        = errors.New("live listing")
)

// FieldErrors maps a field to what's wrong with it.
type FieldErrors map[string]string

func (f FieldErrors) Error() string { return "invalid fields" }

type kind int

const (
	kText kind = iota
	kRequiredText
	kInt
	kYear
	kMoney
	kBool
	kStatus
	kBox
	kRow
	kSlot
	kCategory
)

// What the website may change on a card. Fields the other app doesn't have
// (TCG-only flags on a Sports card) are harmless: the desktop skips them.
var editable = map[string]kind{
	"player_name": kRequiredText, "team": kText, "year": kYear, "season_end": kInt, "brand": kRequiredText,
	"set_name": kRequiredText, "card_number": kRequiredText, "variety": kText, "condition": kRequiredText,
	"estimated_value": kMoney, "sale_price": kMoney, "paid_price": kMoney, "allow_offers": kBool,
	"status": kStatus, "is_rookie": kBool, "is_autograph": kBool, "is_memorabilia": kBool, "is_short_print": kBool,
	"is_error_variant": kBool, "is_blank_back": kBool, "is_chase": kBool, "is_foil": kBool, "is_sketch_card": kBool,
	"grading_company": kText, "grading_cert_number": kText, "card_grade": kText, "serial_number": kText,
	// The Card Editor's Category and markup slider (sale price = estimate + %).
	"sport": kCategory, "sale_price_percent": kInt,
	// Box, row and slot aren't here on purpose: cards are filed (and moved)
	// only in FireCardManager, so a card is never misplaced from a phone.
}

// Value fields the desktop stores as non-nullable numbers: null isn't allowed.
var notNull = map[string]bool{"estimated_value": true, "year": true}

func normalize(k string, kd kind, raw json.RawMessage) (json.RawMessage, string) {
	isNull := strings.TrimSpace(string(raw)) == "null"
	switch kd {
	case kText, kRequiredText, kRow:
		if isNull {
			if kd == kRequiredText {
				return nil, "Required."
			}
			return json.RawMessage("null"), ""
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, "Must be text."
		}
		s = strings.TrimSpace(s)
		if len([]rune(s)) > 200 {
			return nil, "Too long (200 characters at most)."
		}
		if s == "" {
			if kd == kRequiredText {
				return nil, "Required."
			}
			return json.RawMessage("null"), ""
		}
		if kd == kRow {
			s = strings.ToUpper(s)
			if !rowRE.MatchString(s) {
				return nil, "Use letters and numbers, like A or AA."
			}
		}
		b, _ := json.Marshal(s)
		return b, ""
	case kInt, kYear, kSlot:
		if isNull {
			if notNull[k] {
				return nil, "Required."
			}
			return json.RawMessage("null"), ""
		}
		var f float64
		if json.Unmarshal(raw, &f) != nil || f != math.Trunc(f) {
			return nil, "Must be a whole number."
		}
		switch {
		case kd == kYear && (f < 1800 || f > 2100):
			return nil, "Enter a year like 2024."
		case kd == kSlot && (f < 1 || f > 9999):
			return nil, "Slot must be 1 or more."
		case kd == kInt && (f < 0 || f > 9999):
			return nil, "Out of range."
		}
		return json.RawMessage(fmt.Sprint(int(f))), ""
	case kMoney:
		if isNull {
			if notNull[k] {
				return nil, "Required."
			}
			return json.RawMessage("null"), ""
		}
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return nil, "Must be a number."
		}
		if f < 0 || f > 1_000_000 {
			return nil, "Enter an amount from 0 to 1,000,000."
		}
		f = math.Round(f*100) / 100
		return json.RawMessage(fmt.Sprintf("%.2f", f)), ""
	case kBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, "Must be yes or no."
		}
		return json.RawMessage(fmt.Sprint(b)), ""
	case kStatus:
		var s string
		if json.Unmarshal(raw, &s) != nil || (s != "NotForSale" && s != "Ready") {
			return nil, "Choose Ready to list or Not for sale."
		}
		b, _ := json.Marshal(s)
		return b, ""
	case kCategory:
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) > 40 || !rowRE.MatchString(strings.ReplaceAll(s, " ", "")) {
			return nil, "Pick a category."
		}
		b, _ := json.Marshal(strings.TrimSpace(s))
		return b, ""
	case kBox:
		if isNull {
			return json.RawMessage("null"), ""
		}
		var s string
		if json.Unmarshal(raw, &s) != nil || !uuidRE.MatchString(s) {
			return nil, "Pick a box."
		}
		b, _ := json.Marshal(strings.ToLower(s))
		return b, ""
	}
	return nil, "Can't be changed here."
}

// DeleteCard removes a card from the inventory everywhere (the desktop
// deletes it on its next sync), like the Card Editor's Delete.
func (s *Service) DeleteCard(ctx context.Context, accountID, id string) error {
	c, err := s.Card(ctx, accountID, id)
	if err != nil {
		return err
	}
	var cur CardData
	_ = json.Unmarshal(c.Data, &cur)
	if cur.Status == soldStatus {
		return ErrSoldAlready
	}
	var listing struct {
		MarketplaceListingID *string `json:"marketplace_listing_id"`
	}
	_ = json.Unmarshal(c.Data, &listing)
	if cur.Status == "EBA" || cur.Status == "EBF" || (listing.MarketplaceListingID != nil && *listing.MarketplaceListingID != "") {
		return ErrLive
	}
	_, err = s.push(ctx, s.device(c, accountID), cloudsync.Change{Entity: "card", ID: c.ID, BaseVersion: c.Version, Deleted: true, EditedAt: time.Now()})
	return err
}

// EditResult is what the website shows after saving.
type EditResult struct {
	Card Card     `json:"card"`
	Lost []string `json:"lost_fields,omitempty"`
}

func (s *Service) device(c Card, accountID string) cloudsync.Device {
	return cloudsync.Device{AccountID: accountID, Edition: c.Edition}
}

func (s *Service) push(ctx context.Context, d cloudsync.Device, ch cloudsync.Change) (cloudsync.Result, error) {
	res, err := s.Sync.Push(ctx, d, []cloudsync.Change{ch})
	if err != nil {
		return cloudsync.Result{}, err
	}
	if len(res) != 1 {
		return cloudsync.Result{}, errors.New("sync: no result")
	}
	if res[0].Status == "rejected" {
		return res[0], fmt.Errorf("sync rejected the change: %s", res[0].Reason)
	}
	return res[0], nil
}

func timeJSON(t time.Time) json.RawMessage {
	b, _ := json.Marshal(t.UTC().Format("2006-01-02T15:04:05.0000000Z"))
	return b
}

// EditCard changes fields on one card. baseVersion is the version the
// seller was looking at: a field someone changed on a PC since then is
// still overwritten (the website edit is newer) but recorded on the
// conflicts page.
func (s *Service) EditCard(ctx context.Context, accountID, id string, baseVersion int, in map[string]json.RawMessage) (EditResult, error) {
	c, err := s.Card(ctx, accountID, id)
	if err != nil {
		return EditResult{}, err
	}
	var cur CardData
	_ = json.Unmarshal(c.Data, &cur)
	if cur.Status == soldStatus {
		return EditResult{}, ErrSoldAlready
	}

	fields := map[string]json.RawMessage{}
	bad := FieldErrors{}
	for k, raw := range in {
		kd, ok := editable[k]
		if !ok {
			bad[k] = "Can't be changed on the website."
			continue
		}
		v, why := normalize(k, kd, raw)
		if why != "" {
			bad[k] = why
			continue
		}
		fields[k] = v
	}
	if _, ok := fields["status"]; ok && (cur.Status == "EBA" || cur.Status == "EBF") {
		bad["status"] = "This card is live on eBay. End the listing in FireCardManager first."
	}
	if len(bad) > 0 {
		return EditResult{}, bad
	}
	if len(fields) == 0 {
		return EditResult{Card: c}, nil
	}

	// Where the card would end up: the box must be the seller's, and the
	// slot must be free.
	box := c.BoxID
	if v, ok := fields["box_id"]; ok {
		box = ""
		_ = json.Unmarshal(v, &box)
	}
	row, slot := strDefault(cur.SlotRow), cur.SlotNumber
	if v, ok := fields["slot_row"]; ok {
		row = ""
		_ = json.Unmarshal(v, &row)
	}
	if v, ok := fields["slot_number"]; ok {
		slot = nil
		_ = json.Unmarshal(v, &slot)
	}
	if box != "" && box != c.BoxID {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM sync_boxes WHERE id = $1 AND account_id = $2 AND edition = $3 AND deleted_at IS NULL`,
			box, accountID, c.Edition).Scan(&n); err != nil {
			return EditResult{}, err
		}
		if n == 0 {
			return EditResult{}, FieldErrors{"box_id": "That box isn't in your inventory."}
		}
	}
	moved := box != c.BoxID || row != strDefault(cur.SlotRow) || !sameInt(slot, cur.SlotNumber)
	if moved && box != "" && row != "" && slot != nil {
		var other string
		err := s.DB.QueryRowContext(ctx, `SELECT coalesce(data->>'player_name', 'another card') FROM sync_cards
			WHERE account_id = $1 AND box_id = $2 AND coalesce(data->>'slot_row', '') = $3 AND data->'slot_number' = to_jsonb($4::int)
			  AND id <> $5 AND deleted_at IS NULL AND coalesce(status, '') <> $6 LIMIT 1`,
			accountID, box, row, *slot, c.ID, soldStatus).Scan(&other)
		if err == nil {
			return EditResult{}, FieldErrors{"slot_number": fmt.Sprintf("%s is already in row %s, slot %d.", other, row, *slot)}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return EditResult{}, err
		}
	}

	fields["updated_at"] = timeJSON(time.Now())
	r, err := s.push(ctx, s.device(c, accountID), cloudsync.Change{
		Entity: "card", ID: c.ID, BaseVersion: baseVersion, Fields: fields, EditedAt: time.Now(),
	})
	if err != nil {
		return EditResult{}, err
	}
	out, err := s.Card(ctx, accountID, id)
	return EditResult{Card: out, Lost: r.Lost}, err
}

// CardData is the part of a card's fields the server reads itself.
type CardData struct {
	Status        string          `json:"status"`
	SlotRow       *string         `json:"slot_row"`
	SlotNumber    *int            `json:"slot_number"`
	PlayerName    string          `json:"player_name"`
	EbayListingID *string         `json:"ebay_listing_id"`
	Estimated     json.RawMessage `json:"estimated_value"`
}

func strDefault(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Sale is what the seller enters when marking a card sold on the website.
type Sale struct {
	Price    float64   `json:"sale_price"`
	Shipping float64   `json:"shipping_charged"`
	Date     time.Time `json:"sale_date"`
	Where    string    `json:"where"` // in_person, ebay, slabs, other
	Buyer    string    `json:"buyer"`
	Notes    string    `json:"notes"`
}

var saleWhere = map[string]bool{"in_person": true, "ebay": true, "slabs": true, "other": true}

// MarkSold adds the sale to the Sold Log and marks the card Sold. The
// desktop, on its next sync, takes the card out of its inventory, files
// the sale with the card's photos, and ends any eBay or Slabs & Singles
// listing the sale didn't happen on.
func (s *Service) MarkSold(ctx context.Context, accountID, id string, baseVersion int, sale Sale, soldID string) (string, error) {
	c, err := s.Card(ctx, accountID, id)
	if err != nil {
		return "", err
	}
	var cur CardData
	_ = json.Unmarshal(c.Data, &cur)
	if cur.Status == soldStatus {
		return "", ErrSoldAlready
	}
	bad := FieldErrors{}
	if sale.Price < 0 || sale.Price > 1_000_000 {
		bad["sale_price"] = "Enter the amount it sold for."
	}
	if sale.Shipping < 0 || sale.Shipping > 10_000 {
		bad["shipping_charged"] = "Enter the shipping charged, or 0."
	}
	if !saleWhere[sale.Where] {
		bad["where"] = "Choose where it sold."
	}
	now := time.Now()
	if sale.Date.IsZero() {
		sale.Date = now
	}
	if sale.Date.After(now.Add(36*time.Hour)) || sale.Date.Before(now.AddDate(-5, 0, 0)) {
		bad["sale_date"] = "Enter the date it sold."
	}
	if len([]rune(sale.Buyer)) > 200 {
		bad["buyer"] = "Too long."
	}
	if len([]rune(sale.Notes)) > 2000 {
		bad["notes"] = "Too long (2,000 characters at most)."
	}
	if len(bad) > 0 {
		return "", bad
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(c.Data, &data); err != nil {
		return "", err
	}
	copyField := func(dst map[string]json.RawMessage, from, to string) {
		if v, ok := data[from]; ok {
			dst[to] = v
		}
	}
	str := func(s string) json.RawMessage {
		if strings.TrimSpace(s) == "" {
			return json.RawMessage("null")
		}
		b, _ := json.Marshal(strings.TrimSpace(s))
		return b
	}
	money := func(f float64) json.RawMessage { return json.RawMessage(fmt.Sprintf("%.2f", math.Round(f*100)/100)) }

	sold := map[string]json.RawMessage{}
	for _, k := range []string{"sport", "player_name", "team", "year", "brand", "set_name", "card_number", "variety", "condition", "sku",
		"front_photo_id", "back_photo_id", "front_thumb_photo_id", "back_thumb_photo_id"} {
		copyField(sold, k, k)
	}
	copyField(sold, "estimated_value", "estimated_value_at_sale")
	sold["sale_price"] = money(sale.Price)
	sold["shipping_charged"] = money(sale.Shipping)
	sold["buyer"] = str(sale.Buyer)
	sold["notes"] = str(sale.Notes)
	sold["sale_date"] = timeJSON(sale.Date)
	sold["sold_outside_ebay"] = json.RawMessage(fmt.Sprint(sale.Where != "ebay"))
	sold["archived"] = json.RawMessage("false")
	sold["ebay_listing_ended"] = json.RawMessage("false")
	sold["sold_where"] = str(sale.Where)
	sold["card_id"] = str(c.ID)
	if sale.Where == "ebay" {
		copyField(sold, "ebay_listing_id", "ebay_listing_id")
	}

	d := s.device(c, accountID)
	if _, err := s.push(ctx, d, cloudsync.Change{Entity: "sold", ID: soldID, Fields: sold, EditedAt: now}); err != nil {
		return "", err
	}
	soldIDJSON, _ := json.Marshal(soldID)
	r, err := s.push(ctx, d, cloudsync.Change{Entity: "card", ID: c.ID, BaseVersion: baseVersion, EditedAt: now, Fields: map[string]json.RawMessage{
		"status": json.RawMessage(`"Sold"`), "sold_entry_id": soldIDJSON, "sold_where": str(sale.Where), "updated_at": timeJSON(now),
	}})
	if err == nil && len(r.Lost) > 0 {
		// Sold on a PC in the meantime: drop the duplicate sale.
		_, err = s.push(ctx, d, cloudsync.Change{Entity: "sold", ID: soldID, BaseVersion: 1, Deleted: true, EditedAt: now})
		if err == nil {
			err = ErrSoldAlready
		}
	}
	return soldID, err
}

// ---------------- conflicts ----------------

type Conflict struct {
	ID         int64           `json:"id"`
	Entity     string          `json:"entity"`
	EntityID   string          `json:"entity_id"`
	Name       string          `json:"name"`
	Field      string          `json:"field"`
	Kept       json.RawMessage `json:"kept"`
	Lost       json.RawMessage `json:"lost"`
	LostDevice string          `json:"lost_device,omitempty"` // PC name, "" for the website
	LostAt     *time.Time      `json:"lost_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	Current    json.RawMessage `json:"current"` // the field's value now
	Gone       bool            `json:"gone"`    // the row has since been deleted
}

func (s *Service) Conflicts(ctx context.Context, accountID string) ([]Conflict, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT k.id, k.entity, k.entity_id, k.field, coalesce(k.kept_value, 'null'), coalesce(k.lost_value, 'null'),
			coalesce(d.name, ''), k.lost_edited_at, k.created_at,
			coalesce(c.data->>'player_name', s.data->>'player_name', b.data->>'name', ''),
			coalesce(c.data->k.field, s.data->k.field, b.data->k.field, 'null'),
			coalesce(c.deleted_at, s.deleted_at, b.deleted_at) IS NOT NULL OR (c.id IS NULL AND s.id IS NULL AND b.id IS NULL)
		FROM sync_conflicts k
		LEFT JOIN devices d ON d.id = k.lost_device
		LEFT JOIN sync_cards c ON k.entity = 'card' AND c.id = k.entity_id AND c.account_id = k.account_id
		LEFT JOIN sync_sold s ON k.entity = 'sold' AND s.id = k.entity_id AND s.account_id = k.account_id
		LEFT JOIN sync_boxes b ON k.entity = 'box' AND b.id = k.entity_id AND b.account_id = k.account_id
		WHERE k.account_id = $1 AND k.resolved_at IS NULL
		ORDER BY k.created_at DESC, k.id DESC
		LIMIT 500`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conflict{}
	for rows.Next() {
		var c Conflict
		var kept, lost, cur []byte
		var lostAt sql.NullTime
		if err := rows.Scan(&c.ID, &c.Entity, &c.EntityID, &c.Field, &kept, &lost, &c.LostDevice, &lostAt, &c.CreatedAt,
			&c.Name, &cur, &c.Gone); err != nil {
			return nil, err
		}
		c.Kept, c.Lost, c.Current = kept, lost, cur
		if lostAt.Valid {
			c.LostAt = &lostAt.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveConflict closes a conflict. useLost puts the value that lost back
// (as a new edit from the website); otherwise the kept value stays.
func (s *Service) ResolveConflict(ctx context.Context, accountID string, id int64, useLost bool) error {
	var entity, entityID, field string
	var lost []byte
	err := s.DB.QueryRowContext(ctx, `SELECT entity, entity_id, field, coalesce(lost_value, 'null') FROM sync_conflicts
		WHERE id = $1 AND account_id = $2 AND resolved_at IS NULL`, id, accountID).Scan(&entity, &entityID, &field, &lost)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if useLost {
		var edition string
		var version int
		var gone bool
		table := map[string]string{"card": "sync_cards", "sold": "sync_sold", "box": "sync_boxes"}[entity]
		err := s.DB.QueryRowContext(ctx, `SELECT edition, version, deleted_at IS NOT NULL FROM `+table+` WHERE id = $1 AND account_id = $2`,
			entityID, accountID).Scan(&edition, &version, &gone)
		if errors.Is(err, sql.ErrNoRows) || gone {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		fields := map[string]json.RawMessage{field: lost}
		if entity == "card" {
			fields["updated_at"] = timeJSON(time.Now())
		}
		if _, err := s.push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, cloudsync.Change{
			Entity: entity, ID: entityID, BaseVersion: version, Fields: fields, EditedAt: time.Now(),
		}); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE sync_conflicts SET resolved_at = now() WHERE id = $1 AND account_id = $2`, id, accountID)
	return err
}

// Boxes lists the seller's boxes for the move picker.
func (s *Service) Boxes(ctx context.Context, accountID, edition string) ([]Box, error) {
	sum, err := s.Summary(ctx, accountID, edition)
	return sum.Boxes, err
}
