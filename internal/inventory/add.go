package inventory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/identify"
)

// Adding cards from the website (a phone camera): the seller's own AI key
// names the card, the seller checks it, and the card is created through the
// sync engine so the PC pulls it (with its photos) like any other change.

const (
	aiKeyPurpose     = "ai-key"
	IdentifyPerHour  = 600 // the customer's own AI key pays; this only guards the server
	RowSize          = 50  // cards per row, like the desktop's filing
	maxIdentifyPhoto = 4 << 20
)

var (
	ErrNoAIKey     = errors.New("no AI key")
	ErrTooMany     = errors.New("too many identifications this hour")
	ErrBadProvider = errors.New("choose Claude or Gemini")
)

// Sealer encrypts the AI key at rest (auth.MFA).
type Sealer interface {
	SealFor(purpose string, secret []byte) ([]byte, error)
	OpenFor(purpose string, box []byte) ([]byte, error)
}

// Identifier calls the AI service (identify.Client).
type Identifier interface {
	Identify(ctx context.Context, provider, key, model, edition string, front, back []byte) (identify.Result, error)
}

type AIKeyInfo struct {
	Provider   string    `json:"provider"`
	Model      string    `json:"model,omitempty"`
	Last4      string    `json:"last4"`
	Source     string    `json:"source"` // pc or web
	DeviceName string    `json:"device_name,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SaveAIKey stores (or replaces) the account's AI key. From a PC, a key the
// seller typed on the website is kept: the website's choice wins.
func (s *Service) SaveAIKey(ctx context.Context, accountID, provider, key, model, source, deviceID string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	key = strings.TrimSpace(key)
	model = strings.TrimSpace(model)
	if provider != "claude" && provider != "gemini" {
		return ErrBadProvider
	}
	if len(key) < 20 || len(key) > 300 || strings.ContainsAny(key, " \t\r\n") {
		return FieldErrors{"key": "That doesn't look like an API key."}
	}
	if len(model) > 100 {
		return FieldErrors{"model": "Too long."}
	}
	box, err := s.Sealer.SealFor(aiKeyPurpose, []byte(key))
	if err != nil {
		return err
	}
	q := `INSERT INTO account_ai_keys (account_id, provider, key_enc, key_last4, model, source, device_id, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, now())
		ON CONFLICT (account_id) DO UPDATE SET provider = EXCLUDED.provider, key_enc = EXCLUDED.key_enc,
			key_last4 = EXCLUDED.key_last4, model = EXCLUDED.model, source = EXCLUDED.source,
			device_id = EXCLUDED.device_id, updated_at = now()`
	if source == "pc" {
		q += ` WHERE account_ai_keys.source = 'pc'`
	}
	var dev any
	if deviceID != "" {
		dev = deviceID
	}
	_, err = s.DB.ExecContext(ctx, q, accountID, provider, box, key[len(key)-4:], model, source, dev)
	return err
}

// DeleteAIKey removes the key; source "pc" removes only a key a PC sent.
func (s *Service) DeleteAIKey(ctx context.Context, accountID, source string) error {
	q := `DELETE FROM account_ai_keys WHERE account_id = $1`
	if source == "pc" {
		q += ` AND source = 'pc'`
	}
	_, err := s.DB.ExecContext(ctx, q, accountID)
	return err
}

func (s *Service) AIKey(ctx context.Context, accountID string) (*AIKeyInfo, error) {
	var k AIKeyInfo
	var model sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT k.provider, k.model, k.key_last4, k.source, coalesce(d.name, ''), k.updated_at
		FROM account_ai_keys k LEFT JOIN devices d ON d.id = k.device_id WHERE k.account_id = $1`, accountID).
		Scan(&k.Provider, &model, &k.Last4, &k.Source, &k.DeviceName, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.Model = model.String
	return &k, nil
}

// IdentifyResult is what the Add card page fills its form with.
type IdentifyResult struct {
	Fields     map[string]any `json:"fields"`
	Notes      string         `json:"notes,omitempty"`
	Confidence *float64       `json:"confidence,omitempty"`
	Provider   string         `json:"provider"`
}

// Identify names a card from its photos with the account's AI key.
func (s *Service) Identify(ctx context.Context, accountID, edition string, front, back []byte) (IdentifyResult, error) {
	if len(front) == 0 || len(back) == 0 || len(front) > maxIdentifyPhoto || len(back) > maxIdentifyPhoto {
		return IdentifyResult{}, FieldErrors{"photos": "Take a photo of the front and the back."}
	}
	var provider, model string
	var box []byte
	var mdl sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT provider, key_enc, model FROM account_ai_keys WHERE account_id = $1`, accountID).
		Scan(&provider, &box, &mdl)
	if errors.Is(err, sql.ErrNoRows) {
		return IdentifyResult{}, ErrNoAIKey
	}
	if err != nil {
		return IdentifyResult{}, err
	}
	model = mdl.String
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM identify_log WHERE account_id = $1 AND created_at > now() - interval '1 hour'`,
		accountID).Scan(&n); err != nil {
		return IdentifyResult{}, err
	}
	if n >= IdentifyPerHour {
		return IdentifyResult{}, ErrTooMany
	}
	key, err := s.Sealer.OpenFor(aiKeyPurpose, box)
	if err != nil {
		return IdentifyResult{}, fmt.Errorf("open AI key: %w", err)
	}
	r, err := s.AI.Identify(ctx, provider, string(key), model, edition, front, back)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO identify_log (account_id, ok) VALUES ($1, $2)`, accountID, err == nil)
	if err != nil {
		return IdentifyResult{}, err
	}
	out := IdentifyResult{Fields: identify.Fields(r, edition, time.Now()), Confidence: r.Confidence, Provider: provider}
	if r.Notes != nil {
		out.Notes = *r.Notes
	}
	return out, nil
}

// Fields a new card may start with, beyond what Edit allows.
var createExtra = map[string]bool{"sport": true, "sale_price_percent": true, "last_priced_at": true}

// CreateCard adds a card made on the website. With a box but no slot, it
// goes after the box's last card (a new row after 50), like the desktop.
// WebSKU is a SKU for a card made on the website by a seller without a PC:
// FCMW- and 8 letters and digits (no 0/O or 1/I).
func WebSKU() string {
	const abc = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return "FCMW-" + string(b)
}

// PendingName is the desktop's placeholder for a card not identified yet.
const PendingName = "(unidentified - pending ID)"

// ImportOptions: a card from a CSV import, as the desktop's importer takes
// it. Only the category (and the photos) are required; blank details are
// filled in later. A BoxName that matches one of the seller's boxes puts the
// card after that box's last card; otherwise it's filed by category.
type ImportOptions struct {
	BoxName string
}

func (s *Service) CreateCard(ctx context.Context, accountID, edition, id string, in map[string]json.RawMessage) (Card, error) {
	return s.createCard(ctx, accountID, edition, id, in, nil)
}

// ImportCard adds one card from a CSV import (see ImportOptions).
func (s *Service) ImportCard(ctx context.Context, accountID, edition, id string, in map[string]json.RawMessage, o ImportOptions) (Card, error) {
	return s.createCard(ctx, accountID, edition, id, in, &o)
}

func (s *Service) createCard(ctx context.Context, accountID, edition, id string, in map[string]json.RawMessage, imp *ImportOptions) (Card, error) {
	if edition != "sports" && edition != "tcg" {
		return Card{}, FieldErrors{"edition": "Choose Sports or TCG."}
	}
	fields := map[string]json.RawMessage{}
	bad := FieldErrors{}
	for k, raw := range in {
		if createExtra[k] {
			switch k {
			case "sport":
				var v string
				if json.Unmarshal(raw, &v) != nil || len(v) > 40 || !rowRE.MatchString(strings.ReplaceAll(v, " ", "")) {
					bad[k] = "Pick a category."
					continue
				}
			case "sale_price_percent":
				var v float64
				if json.Unmarshal(raw, &v) != nil || v < 0 || v > 1000 {
					bad[k] = "Out of range."
					continue
				}
				raw = json.RawMessage(fmt.Sprint(int(v)))
			case "last_priced_at":
				var v time.Time
				if json.Unmarshal(raw, &v) != nil {
					continue
				}
				raw = timeJSON(v)
			}
			fields[k] = raw
			continue
		}
		kd, ok := editable[k]
		if !ok {
			bad[k] = "Can't be set on the website."
			continue
		}
		v, why := normalize(k, kd, raw)
		if why != "" {
			bad[k] = why
			continue
		}
		fields[k] = v
	}
	if imp != nil {
		// Blank details are fine on import, like the desktop's importer.
		defaults := map[string]string{"player_name": `"` + PendingName + `"`, "brand": `""`, "set_name": `""`, "card_number": `""`,
			"condition": `"Raw - NM"`, "year": "0"}
		for k, v := range defaults {
			if _, ok := fields[k]; !ok && (bad[k] == "" || bad[k] == "Required.") {
				delete(bad, k)
				fields[k] = json.RawMessage(v)
			}
		}
	}
	for _, k := range []string{"player_name", "brand", "set_name", "card_number", "condition"} {
		if _, ok := fields[k]; !ok && bad[k] == "" {
			bad[k] = "Required."
		}
	}
	if _, ok := fields["year"]; !ok && bad["year"] == "" {
		bad["year"] = "Required."
	}
	if len(bad) > 0 {
		return Card{}, bad
	}
	if _, ok := fields["status"]; !ok {
		fields["status"] = json.RawMessage(`"NotForSale"`)
		if p, ok := fields["sale_price"]; ok && string(p) != "null" && string(p) != "0.00" {
			fields["status"] = json.RawMessage(`"Ready"`)
		}
	}
	for _, k := range []string{"estimated_value"} {
		if _, ok := fields[k]; !ok {
			fields[k] = json.RawMessage("0")
		}
	}
	for _, k := range []string{"is_rookie", "is_autograph", "is_memorabilia", "is_short_print", "is_error_variant", "is_blank_back", "allow_offers"} {
		if _, ok := fields[k]; !ok {
			fields[k] = json.RawMessage("false")
		}
	}

	// Where it goes: always filed automatically, like FireCardManager does
	// (location is changed only in the desktop app, so cards aren't misplaced).
	var category string
	if v, ok := fields["sport"]; ok {
		_ = json.Unmarshal(v, &category)
	}
	if category == "" {
		return Card{}, FieldErrors{"sport": "Pick the category; the card is filed in that box."}
	}
	d := cloudsync.Device{AccountID: accountID, Edition: edition}
	unlock, err := s.lockFiling(ctx, accountID)
	if err != nil {
		return Card{}, err
	}
	defer unlock()
	var box, row string
	var slot int
	if imp != nil && strings.TrimSpace(imp.BoxName) != "" {
		box, row, slot, err = s.namedBoxSpot(ctx, d, imp.BoxName)
		if err != nil {
			return Card{}, err
		}
	}
	if box == "" {
		box, row, slot, err = s.FileSpot(ctx, d, category)
		if err != nil {
			return Card{}, err
		}
	}
	fields["box_id"], _ = json.Marshal(box)
	fields["slot_row"], _ = json.Marshal(row)
	fields["slot_number"] = json.RawMessage(fmt.Sprint(slot))

	// A seller with no PC for this app gets a web SKU (the desktop names its
	// own cards FCM-<number>), so the card can be listed and exported.
	var pcs int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE account_id = $1 AND edition = $2 AND revoked_at IS NULL`,
		accountID, edition).Scan(&pcs); err != nil {
		return Card{}, err
	}
	if pcs == 0 {
		fields["sku"], _ = json.Marshal(WebSKU())
	}
	now := time.Now()
	fields["created_at"], fields["updated_at"] = timeJSON(now), timeJSON(now)
	r, err := s.push(ctx, d, cloudsync.Change{
		Entity: "card", ID: id, Fields: fields, EditedAt: now,
	})
	if err != nil {
		if r.Reason == "card_limit" {
			return Card{}, FieldErrors{"card": fmt.Sprintf("Your online inventory is at its %d-card limit.", cloudsync.CardLimit)}
		}
		return Card{}, err
	}
	return s.Card(ctx, accountID, id)
}

// FireCardManager's filing: a box per category named after it ("Basketball",
// then "Basketball (cont. 2)", ...), rows A..Z of 50 slots. A new card goes
// right after the box's last card - never into an old hole - and into the
// next box of that category (made if needed) when the box is full.
const (
	SlotsPerRow   = 50
	MaxRowsPerBox = 26
)

// lockFiling keeps two cards added at once from getting the same slot.
func (s *Service) lockFiling(ctx context.Context, accountID string) (func(), error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('filing:' || $1))`, accountID); err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('filing:' || $1))`, accountID)
		conn.Close()
	}, nil
}

// FileSpot picks the box, row and slot for a new card of this category,
// creating the category's box (through sync, so PCs get it) when needed.
func (s *Service) FileSpot(ctx context.Context, d cloudsync.Device, category string) (string, string, int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, data->>'name' FROM sync_boxes
		WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL
		  AND lower(split_part(coalesce(data->>'name', ''), ' (cont. ', 1)) = lower($3)
		ORDER BY coalesce(data->>'created_at', ''), updated_at, id`, d.AccountID, d.Edition, category)
	if err != nil {
		return "", "", 0, err
	}
	type cand struct{ id, name string }
	var boxes []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.name); err != nil {
			rows.Close()
			return "", "", 0, err
		}
		boxes = append(boxes, c)
	}
	rows.Close()
	names := map[string]bool{}
	for _, b := range boxes {
		names[strings.ToLower(b.name)] = true
		row, slot, ok, err := s.appendSlot(ctx, d.AccountID, b.id)
		if err != nil {
			return "", "", 0, err
		}
		if ok {
			return b.id, row, slot, nil
		}
	}
	// No box with room: a new one, named like the desktop names them.
	name := category
	for n := 2; names[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s (cont. %d)", category, n)
	}
	id := NewID()
	nameJSON, _ := json.Marshal(name)
	if _, err := s.push(ctx, d, cloudsync.Change{Entity: "box", ID: id, EditedAt: time.Now(), Fields: map[string]json.RawMessage{
		"name": nameJSON, "description": json.RawMessage("null"), "color_theme": json.RawMessage("null"), "created_at": timeJSON(time.Now()),
	}}); err != nil {
		return "", "", 0, err
	}
	return id, RowLetter(0), 1, nil
}

// namedBoxSpot is the slot after the last card of the seller's box with
// this name ("" when there's no such box or it's full).
func (s *Service) namedBoxSpot(ctx context.Context, d cloudsync.Device, name string) (string, string, int, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM sync_boxes WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL
		AND lower(coalesce(data->>'name', '')) = lower($3) ORDER BY updated_at LIMIT 1`, d.AccountID, d.Edition, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, nil
	}
	if err != nil {
		return "", "", 0, err
	}
	row, slot, ok, err := s.appendSlot(ctx, d.AccountID, id)
	if err != nil || !ok {
		return "", "", 0, err
	}
	return id, row, slot, nil
}

// appendSlot is the slot after the box's last card; ok is false when the
// box is full at the end.
func (s *Service) appendSlot(ctx context.Context, accountID, box string) (string, int, bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data->>'slot_row', CASE WHEN jsonb_typeof(data->'slot_number') = 'number' THEN (data->>'slot_number')::int END
		FROM sync_cards WHERE account_id = $1 AND box_id = $2 AND deleted_at IS NULL
		  AND coalesce(data->>'slot_row', '') <> '' AND jsonb_typeof(data->'slot_number') = 'number'`, accountID, box)
	if err != nil {
		return "", 0, false, err
	}
	defer rows.Close()
	lastRow, lastSlot := -1, 0
	for rows.Next() {
		var r string
		var n int
		if err := rows.Scan(&r, &n); err != nil {
			return "", 0, false, err
		}
		i := RowIndex(r)
		if i < 0 {
			continue
		}
		if i > lastRow || (i == lastRow && n > lastSlot) {
			lastRow, lastSlot = i, n
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, false, err
	}
	switch {
	case lastRow < 0:
		return RowLetter(0), 1, true, nil
	case lastSlot < SlotsPerRow:
		return RowLetter(lastRow), lastSlot + 1, true, nil
	case lastRow+1 < MaxRowsPerBox:
		return RowLetter(lastRow + 1), 1, true, nil
	}
	return "", 0, false, nil
}

// RowLetter: 0 -> A, 25 -> Z, 26 -> AA (the desktop's RowLetterHelper).
func RowLetter(i int) string {
	n := i + 1
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

// RowIndex is RowLetter's inverse; -1 for a row with no letters.
func RowIndex(r string) int {
	v := 0
	for _, c := range strings.ToUpper(r) {
		if c < 'A' || c > 'Z' {
			continue
		}
		v = v*26 + int(c-'A'+1)
	}
	return v - 1
}
