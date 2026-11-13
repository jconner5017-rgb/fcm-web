package inventory

import (
	"context"
	"encoding/json"
	"time"
)

// The desktop's Dashboard (DashboardStatsService), worked out from the
// synced cards and Sold Log so both show the same numbers.

const pendingName = "(unidentified - pending ID)" // CardService.PendingIdentificationPlaceholder

type DayTotal struct {
	Day     string  `json:"day"` // 2026-11-13 (UTC)
	Revenue float64 `json:"revenue"`
	Units   int     `json:"units"`
}

type Dashboard struct {
	Edition string `json:"edition"`

	PricedInventoryValue      float64 `json:"priced_inventory_value"`
	PricedCount               int     `json:"priced_count"`
	TotalActiveCards          int     `json:"total_active_cards"`
	ReadyToListCount          int     `json:"ready_to_list_count"`
	ReadyToListPotentialValue float64 `json:"ready_to_list_potential_value"`
	LiveCount                 int     `json:"live_count"`
	EbayLiveCount             int     `json:"ebay_live_count"`
	SlabsLiveCount            int     `json:"slabs_live_count"`
	BothLiveCount             int     `json:"both_live_count"`
	EbaySold7D                int     `json:"ebay_sold_7d"`
	SlabsSold7D               int     `json:"slabs_sold_7d"`
	Revenue7D                 float64 `json:"revenue_7d"`
	RevenuePrevious7D         float64 `json:"revenue_previous_7d"`
	UnitsSold7D               int     `json:"units_sold_7d"`
	UnitsSoldPrevious7D       int     `json:"units_sold_previous_7d"`
	AiDocumentedCount         int     `json:"ai_documented_count"`
	NeedsIdentificationCount  int     `json:"needs_identification_count"`
	MissingPricingCount       int     `json:"missing_pricing_count"`
	ListingErrorsCount        int     `json:"listing_errors_count"`
	SellThroughRate           float64 `json:"sell_through_rate"`

	Sales31D []DayTotal `json:"sales_31d"`
	Recent   []CardRow  `json:"recent"`
}

type dashCard struct {
	Status         string   `json:"status"`
	PlayerName     string   `json:"player_name"`
	EstimatedValue float64  `json:"estimated_value"`
	SalePrice      *float64 `json:"sale_price"`
	StartBid       *float64 `json:"start_bid"`
	Marketplace    *string  `json:"marketplace_listing_id"`
}

type dashSold struct {
	SalePrice   float64   `json:"sale_price"`
	Shipping    float64   `json:"shipping_charged"`
	SaleDate    time.Time `json:"sale_date"`
	EbayOrder   *string   `json:"ebay_order_id"`
	SlabsOrder  *string   `json:"marketplace_order_id"`
	dateIsValid bool
}

func (d *dashSold) UnmarshalJSON(b []byte) error {
	var raw struct {
		SalePrice  float64 `json:"sale_price"`
		Shipping   float64 `json:"shipping_charged"`
		SaleDate   string  `json:"sale_date"`
		EbayOrder  *string `json:"ebay_order_id"`
		SlabsOrder *string `json:"marketplace_order_id"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	d.SalePrice, d.Shipping, d.EbayOrder, d.SlabsOrder = raw.SalePrice, raw.Shipping, raw.EbayOrder, raw.SlabsOrder
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.9999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, raw.SaleDate); err == nil {
			d.SaleDate, d.dateIsValid = t, true
			break
		}
	}
	return nil
}

func nonEmpty(p *string) bool { return p != nil && *p != "" }

func (s *Service) Dashboard(ctx context.Context, accountID, edition string, now time.Time) (Dashboard, error) {
	out := Dashboard{Edition: edition, Sales31D: []DayTotal{}, Recent: []CardRow{}}

	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM sync_cards
		WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL AND coalesce(status, '') <> $3`, accountID, edition, soldStatus)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		var c dashCard
		_ = json.Unmarshal(raw, &c)
		out.TotalActiveCards++
		sale := 0.0
		if c.SalePrice != nil {
			sale = *c.SalePrice
		}
		priced := c.EstimatedValue > 0 || sale > 0
		onEbay := c.Status == "EBA" || c.Status == "EBF"
		onSlabs := nonEmpty(c.Marketplace)
		if priced {
			out.PricedCount++
			if c.EstimatedValue > 0 {
				out.PricedInventoryValue += c.EstimatedValue
			} else {
				out.PricedInventoryValue += sale
			}
		}
		if c.Status == "Ready" && !onSlabs {
			out.ReadyToListCount++
			if c.SalePrice != nil {
				out.ReadyToListPotentialValue += sale
			} else {
				out.ReadyToListPotentialValue += c.EstimatedValue
			}
		}
		if onEbay || onSlabs {
			out.LiveCount++
		}
		if onEbay {
			out.EbayLiveCount++
		}
		if onSlabs {
			out.SlabsLiveCount++
		}
		if onEbay && onSlabs {
			out.BothLiveCount++
		}
		if c.PlayerName == pendingName {
			out.NeedsIdentificationCount++
		} else {
			out.AiDocumentedCount++
			if !priced {
				out.MissingPricingCount++
			}
		}
		if c.Status == "Ready" || onEbay || onSlabs {
			if c.Status == "EBA" {
				if c.StartBid == nil || *c.StartBid <= 0 {
					out.ListingErrorsCount++
				}
			} else if sale <= 0 {
				out.ListingErrorsCount++
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Sales: last 7 days vs the 7 before, and day by day for 31 days.
	start7, prev7 := now.AddDate(0, 0, -7), now.AddDate(0, 0, -14)
	start31 := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)
	days := map[string]*DayTotal{}
	for i := 0; i < 31; i++ {
		d := start31.AddDate(0, 0, i).Format("2006-01-02")
		out.Sales31D = append(out.Sales31D, DayTotal{Day: d})
	}
	for i := range out.Sales31D {
		days[out.Sales31D[i].Day] = &out.Sales31D[i]
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT data FROM sync_sold WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL
		AND coalesce(data->>'sale_date', '') >= $3`, accountID, edition, prev7.AddDate(0, 0, -20).Format("2006-01-02"))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		var sd dashSold
		if json.Unmarshal(raw, &sd) != nil || !sd.dateIsValid {
			continue
		}
		net := sd.SalePrice + sd.Shipping // the desktop's NetProceeds
		switch {
		case !sd.SaleDate.Before(start7):
			out.Revenue7D += net
			out.UnitsSold7D++
			if nonEmpty(sd.EbayOrder) {
				out.EbaySold7D++
			}
			if nonEmpty(sd.SlabsOrder) {
				out.SlabsSold7D++
			}
		case !sd.SaleDate.Before(prev7):
			out.RevenuePrevious7D += net
			out.UnitsSoldPrevious7D++
		}
		if d := days[sd.SaleDate.UTC().Format("2006-01-02")]; d != nil {
			d.Revenue += net
			d.Units++
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if n := out.ReadyToListCount + out.LiveCount + out.UnitsSold7D; n > 0 {
		out.SellThroughRate = float64(out.UnitsSold7D) / float64(n) * 100
	}

	recent, err := s.Cards(ctx, accountID, Query{Edition: edition, Sort: "added", Desc: true, PerPage: 8})
	if err == nil {
		out.Recent = recent.Cards
	}
	return out, nil
}
