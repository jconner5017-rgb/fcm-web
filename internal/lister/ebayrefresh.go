package lister

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/ebay"
)

// EbayRefresh is what "Refresh from eBay" found (LiveListingsWindow's summary).
type EbayRefresh struct {
	Live         int              `json:"live"`          // live listings eBay reports
	Matched      int              `json:"matched"`       // of them, cards in this inventory
	MarkedLive   int              `json:"marked_live"`   // cards that weren't showing live and now do
	PriceChanges []EbayPriceDelta `json:"price_changes"` // prices changed on eBay, copied to the card
	Orphans      []ebay.LiveOffer `json:"orphans"`       // live on eBay, no card here with that SKU
	NotLive      []string         `json:"not_live"`      // SKUs marked live here that eBay doesn't have live
}

type EbayPriceDelta struct {
	CardID string   `json:"card_id"`
	Sku    string   `json:"sku"`
	Name   string   `json:"name"`
	Old    *float64 `json:"old"`
	New    float64  `json:"new"`
}

// RefreshEbay pulls every live listing from the seller's eBay account and
// marks the matching cards live (status, offer, listing id and link), the
// way the desktop's Live Listings refresh does (MarkLiveOnEbay + EbayPriceSync).
func (s *Service) RefreshEbay(ctx context.Context, accountID string) (EbayRefresh, error) {
	out := EbayRefresh{PriceChanges: []EbayPriceDelta{}, Orphans: []ebay.LiveOffer{}, NotLive: []string{}}
	if s.Ebay == nil {
		return out, ebay.ErrNotConfigured
	}
	token, err := s.Ebay.AccessToken(ctx, accountID)
	if err != nil {
		return out, err
	}
	offers, err := s.Ebay.LiveOffers(ctx, token)
	if err != nil {
		return out, err
	}
	out.Live = len(offers)

	seen := map[string]float64{}
	if rows, err := s.DB.QueryContext(ctx, `SELECT sku, price FROM ebay_seen_prices WHERE account_id = $1`, accountID); err == nil {
		for rows.Next() {
			var k string
			var p float64
			if rows.Scan(&k, &p) == nil {
				seen[strings.ToUpper(k)] = p
			}
		}
		rows.Close()
	}

	liveSkus := map[string]bool{}
	j := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	for _, o := range offers {
		sku := strings.ToUpper(strings.TrimSpace(o.SKU))
		liveSkus[sku] = true
		var id, edition string
		var raw []byte
		var version int
		err := s.DB.QueryRowContext(ctx, `SELECT id, edition, data, version FROM sync_cards
			WHERE account_id = $1 AND deleted_at IS NULL AND upper(data->>'sku') = $2
			ORDER BY updated_at DESC LIMIT 1`, accountID, sku).Scan(&id, &edition, &raw, &version)
		if err != nil {
			out.Orphans = append(out.Orphans, o)
			continue
		}
		out.Matched++
		var c ebay.Card
		_ = json.Unmarshal(raw, &c)
		if c.Status == "Sold" {
			continue
		}
		status := "EBF"
		if o.Format == "AUCTION" {
			status = "EBA"
		}
		link := "https://www.ebay.com/itm/" + o.ListingID
		fields := map[string]json.RawMessage{}
		if c.Status != status {
			fields["status"] = j(status)
			if c.Status != "EBA" && c.Status != "EBF" {
				out.MarkedLive++
			}
		}
		if c.EbayOfferID == nil || *c.EbayOfferID != o.OfferID {
			fields["ebay_offer_id"] = j(o.OfferID)
		}
		if c.EbayListingID == nil || *c.EbayListingID != o.ListingID {
			fields["ebay_listing_id"] = j(o.ListingID)
		}
		if c.EbayListingURL == nil || *c.EbayListingURL != link {
			fields["ebay_listing_url"] = j(link)
		}
		// EbayPriceSync: a Buy It Now price changed on eBay since the last look
		// is copied to the card; an unchanged one leaves the card's own price.
		if o.Format != "AUCTION" && o.Price > 0 {
			last, had := seen[sku]
			if (c.SalePrice == nil || math.Abs(*c.SalePrice-o.Price) > 0.004) && !(had && math.Abs(last-o.Price) < 0.005) {
				fields["sale_price"] = j(o.Price)
				out.PriceChanges = append(out.PriceChanges, EbayPriceDelta{CardID: id, Sku: c.Sku, Name: c.PlayerName, Old: c.SalePrice, New: o.Price})
			}
			_, _ = s.DB.ExecContext(ctx, `INSERT INTO ebay_seen_prices (account_id, sku, price) VALUES ($1, $2, $3)
				ON CONFLICT (account_id, sku) DO UPDATE SET price = EXCLUDED.price`, accountID, sku, o.Price)
		}
		if len(fields) == 0 {
			continue
		}
		now := time.Now()
		fields["updated_at"] = j(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
		if _, err := s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
			Entity: "card", ID: id, BaseVersion: version, EditedAt: now, Fields: fields,
		}}); err != nil {
			return out, err
		}
	}

	// Cards shown live here that eBay no longer has live (sold, ended or
	// expired there). Like the desktop, they're reported, not changed.
	rows, err := s.DB.QueryContext(ctx, `SELECT coalesce(data->>'sku', '') FROM sync_cards
		WHERE account_id = $1 AND deleted_at IS NULL AND status IN ('EBA', 'EBF')`, accountID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sku string
			if rows.Scan(&sku) == nil && !liveSkus[strings.ToUpper(sku)] {
				out.NotLive = append(out.NotLive, sku)
			}
		}
	}
	return out, nil
}
