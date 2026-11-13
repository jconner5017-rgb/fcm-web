package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
)

// A sale in the Sold Log, as the website changes it.
type soldRow struct {
	ID      string
	Edition string
	Version int
	Data    json.RawMessage
}

func (s *Service) soldEntry(ctx context.Context, accountID, id string) (soldRow, error) {
	var r soldRow
	if !uuidRE.MatchString(id) {
		return r, ErrNotFound
	}
	err := s.DB.QueryRowContext(ctx, `SELECT id, edition, version, data FROM sync_sold WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		strings.ToLower(id), accountID).Scan(&r.ID, &r.Edition, &r.Version, &r.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// What the Sold Log lets you change (SoldCard's properties).
var soldEditable = map[string]kind{
	"buyer": kText, "notes": kText, "tracking_number": kText, "carrier": kText,
	"sale_price": kMoney, "shipping_charged": kMoney, "archived": kBool,
}

// Money the desktop stores as non-nullable.
var soldNotNull = map[string]bool{"sale_price": true, "shipping_charged": true}

// EditSold changes one sale; every linked PC gets it on its next sync.
func (s *Service) EditSold(ctx context.Context, accountID, id string, in map[string]json.RawMessage) (json.RawMessage, error) {
	r, err := s.soldEntry(ctx, accountID, id)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	bad := FieldErrors{}
	for k, raw := range in {
		switch k {
		case "shipped_date_utc", "sale_date":
			if strings.TrimSpace(string(raw)) == "null" && k == "shipped_date_utc" {
				fields[k] = json.RawMessage("null")
				continue
			}
			var t time.Time
			if json.Unmarshal(raw, &t) != nil {
				var d string
				if json.Unmarshal(raw, &d) != nil {
					bad[k] = "Enter a date."
					continue
				}
				if t, err = time.Parse("2006-01-02", d); err != nil {
					bad[k] = "Enter a date."
					continue
				}
				t = t.Add(12 * time.Hour)
			}
			fields[k] = timeJSON(t)
			continue
		}
		kd, ok := soldEditable[k]
		if !ok {
			bad[k] = "Can't be changed here."
			continue
		}
		if soldNotNull[k] && strings.TrimSpace(string(raw)) == "null" {
			raw = json.RawMessage("0")
		}
		v, why := normalize(k, kd, raw)
		if why != "" {
			bad[k] = why
			continue
		}
		fields[k] = v
	}
	if len(bad) > 0 {
		return nil, bad
	}
	if len(fields) == 0 {
		return r.Data, nil
	}
	if _, err := s.push(ctx, cloudsync.Device{AccountID: accountID, Edition: r.Edition}, cloudsync.Change{
		Entity: "sold", ID: r.ID, BaseVersion: r.Version, Fields: fields, EditedAt: time.Now(),
	}); err != nil {
		return nil, err
	}
	r, err = s.soldEntry(ctx, accountID, id)
	return r.Data, err
}

// DeleteSold removes the log entry (not the card) everywhere, like Delete Selected.
func (s *Service) DeleteSold(ctx context.Context, accountID, id string) error {
	r, err := s.soldEntry(ctx, accountID, id)
	if err != nil {
		return err
	}
	_, err = s.push(ctx, cloudsync.Device{AccountID: accountID, Edition: r.Edition}, cloudsync.Change{
		Entity: "sold", ID: r.ID, BaseVersion: r.Version, Deleted: true, EditedAt: time.Now(),
	})
	return err
}

// ShippedResult: which Slabs & Singles orders to tell (one per order).
type ShippedResult struct {
	Marked      int      `json:"marked"`
	SlabsOrders []string `json:"-"`
}

// MarkShipped is "Mark Selected as Shipped": tracking (optional), carrier
// and today's date on each sale.
func (s *Service) MarkShipped(ctx context.Context, accountID string, ids []string, tracking, carrier string) (ShippedResult, error) {
	var out ShippedResult
	seen := map[string]bool{}
	f := map[string]json.RawMessage{"shipped_date_utc": timeJSON(time.Now())}
	f["tracking_number"], _ = normalize("tracking_number", kText, mustJSON(tracking))
	f["carrier"], _ = normalize("carrier", kText, mustJSON(carrier))
	for _, id := range ids {
		r, err := s.soldEntry(ctx, accountID, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		if _, err := s.push(ctx, cloudsync.Device{AccountID: accountID, Edition: r.Edition}, cloudsync.Change{
			Entity: "sold", ID: r.ID, BaseVersion: r.Version, Fields: f, EditedAt: time.Now(),
		}); err != nil {
			return out, err
		}
		out.Marked++
		var d struct {
			Order *string `json:"marketplace_order_id"`
		}
		_ = json.Unmarshal(r.Data, &d)
		if d.Order != nil && *d.Order != "" {
			o := strings.SplitN(*d.Order, "|", 2)[0]
			if !seen[o] {
				seen[o] = true
				out.SlabsOrders = append(out.SlabsOrders, o)
			}
		}
	}
	return out, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
