//go:build loadtest

// Load test (2026-11-19): 100 accounts x 5,000 cards, the per-account size the
// site is built for (500 users x 5,000 cards). Run with:
//
//	LOAD_DATABASE_URL=postgres://... go test -tags loadtest -run TestLoad -v ./internal/inventory/
//
// LOAD_SEED=1 (re)creates the data first.
package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/db"
)

const loadAccounts, loadCards, loadBoxes = 100, 5000, 7

func seedLoad(t *testing.T, d *sql.DB) {
	ctx := context.Background()
	for _, q := range []string{
		`TRUNCATE accounts CASCADE`,
		fmt.Sprintf(`INSERT INTO accounts (id, email, password_hash, status)
			SELECT gen_random_uuid(), 'load' || g || '@example.com', 'x', 'active' FROM generate_series(1, %d) g`, loadAccounts),
		fmt.Sprintf(`INSERT INTO sync_boxes (id, account_id, edition, data)
			SELECT gen_random_uuid(), a.id, 'sports', jsonb_build_object('name', (ARRAY['Baseball','Basketball','Football','Soccer','Hockey','Racing','Unsorted'])[b],
				'description', '', 'color_theme', 'sky', 'capacity', 3200, 'kind', 'raw')
			FROM accounts a, generate_series(1, %d) b`, loadBoxes),
		// About 1.4 KB of card data and 1 KB of field times per card, like the real rows.
		fmt.Sprintf(`INSERT INTO sync_cards (id, account_id, edition, data, field_meta, box_id, status, updated_at)
			SELECT gen_random_uuid(), a.id, 'sports',
				jsonb_build_object(
					'sport', 'Football', 'player_name', 'Player ' || (g %% 900), 'team', 'Team ' || (g %% 32), 'year', 2010 + g %% 16,
					'brand', 'Panini', 'set_name', (ARRAY['Prizm','Donruss Optic','Select','Mosaic','Topps Chrome'])[1 + g %% 5],
					'card_number', (g %% 400)::text, 'variety', CASE WHEN g %% 4 = 0 THEN 'Silver Prizm' ELSE '' END, 'condition', 'Raw - NM',
					'estimated_value', round((random() * 40)::numeric, 2), 'sale_price', round((random() * 50)::numeric, 2),
					'start_bid', NULL, 'paid_price', 0.5, 'sku', 'FCM-' || g,
					'status', (ARRAY['Ready','Ready','Ready','EBF','EBA','New'])[1 + g %% 6],
					'slot_row', chr(65 + (g / 50) %% 26), 'slot_number', 1 + g %% 50, 'created_at', (now() - (g || ' minutes')::interval)::text,
					'marketplace_listing_id', CASE WHEN g %% 7 = 0 THEN gen_random_uuid()::text ELSE '' END,
					'marketplace_listed_at', CASE WHEN g %% 7 = 0 THEN now()::text END,
					'front_photo_id', gen_random_uuid()::text, 'back_photo_id', gen_random_uuid()::text,
					'front_thumb_photo_id', gen_random_uuid()::text, 'back_thumb_photo_id', gen_random_uuid()::text,
					'is_rookie', g %% 5 = 0, 'is_autograph', false, 'grading_company', '', 'card_grade', '', 'serial_number', '',
					'ebay_listing_url', CASE WHEN g %% 6 IN (3, 4) THEN 'https://www.ebay.com/itm/' || (100000000000 + g) END,
					'card_sight_note', 'CardSight: matched ' || md5(g::text) || ' with 92%% confidence; parallel checked against the checklist; ' || md5((g * 7)::text),
					'card_sight_match_key', md5(g::text), 'market_price', round((random() * 40)::numeric, 2), 'market_price_source', 'CardSight',
					'storage_kind', 'raw', 'allow_offers', true, 'accept_offer_percent', 85, 'reject_offer_percent', 60),
				(SELECT jsonb_object_agg(k, now()::text) FROM unnest(ARRAY['sport','player_name','team','year','brand','set_name','card_number',
					'variety','condition','estimated_value','sale_price','status','slot_row','slot_number','sku','front_photo_id','back_photo_id',
					'marketplace_listing_id','card_sight_note','market_price','storage_kind','allow_offers','accept_offer_percent']) k),
				(SELECT b.id FROM sync_boxes b WHERE b.account_id = a.id ORDER BY b.id OFFSET (g %% %d) LIMIT 1),
				(ARRAY['Ready','Ready','Ready','EBF','EBA','New'])[1 + g %% 6], now()
			FROM accounts a, generate_series(1, %d) g`, loadBoxes, loadCards),
		`INSERT INTO sync_changes (account_id, entity, entity_id, version) SELECT account_id, 'card', id, 1 FROM sync_cards`,
		`VACUUM ANALYZE sync_cards`, `VACUUM ANALYZE sync_boxes`, `VACUUM ANALYZE sync_changes`,
	} {
		start := time.Now()
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q[:40], err)
		}
		t.Logf("seed step %.1fs", time.Since(start).Seconds())
	}
}

func TestLoad(t *testing.T) {
	url := os.Getenv("LOAD_DATABASE_URL")
	if url == "" {
		t.Skip("LOAD_DATABASE_URL not set")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LOAD_SEED") == "1" {
		seedLoad(t, d)
	}
	var acct string
	if err := d.QueryRow(`SELECT account_id FROM sync_cards GROUP BY 1 ORDER BY 1 LIMIT 1`).Scan(&acct); err != nil {
		t.Fatal(err)
	}
	var box string
	d.QueryRow(`SELECT id FROM sync_boxes WHERE account_id = $1 ORDER BY id LIMIT 1`, acct).Scan(&box)
	s := &Service{DB: d}

	type step struct {
		name string
		fn   func() error
	}
	now := time.Now()
	steps := []step{
		{"home: summary (first load)", func() error { s.cache = resultCache{}; _, err := s.Summary(ctx, acct, "sports"); return err }},
		{"home: dashboard (first load)", func() error { s.cache = resultCache{}; _, err := s.Dashboard(ctx, acct, "sports", now); return err }},
		{"home: summary (again)", func() error { _, err := s.Summary(ctx, acct, "sports"); return err }},
		{"home: dashboard (again)", func() error { _, err := s.Dashboard(ctx, acct, "sports", now); return err }},
		{"inventory: page 1 by slot", func() error { _, err := s.Cards(ctx, acct, Query{Edition: "sports", PerPage: 60}); return err }},
		{"inventory: page 40 by player", func() error {
			_, err := s.Cards(ctx, acct, Query{Edition: "sports", Sort: "player", Page: 40, PerPage: 60})
			return err
		}},
		{"inventory: by value desc", func() error {
			_, err := s.Cards(ctx, acct, Query{Edition: "sports", Sort: "value", Desc: true})
			return err
		}},
		{"inventory: search 'player 12 prizm'", func() error {
			_, err := s.Cards(ctx, acct, Query{Edition: "sports", Search: "player 12 prizm"})
			return err
		}},
		{"inventory: one box, row C", func() error { _, err := s.Cards(ctx, acct, Query{Edition: "sports", Box: box, Row: "C"}); return err }},
		{"boxes: tree", func() error { _, err := s.Tree(ctx, acct, "sports"); return err }},
	}
	for _, st := range steps {
		var ms []float64
		for i := 0; i < 5; i++ {
			start := time.Now()
			if err := st.fn(); err != nil {
				t.Fatalf("%s: %v", st.name, err)
			}
			ms = append(ms, float64(time.Since(start).Microseconds())/1000)
		}
		sort.Float64s(ms)
		t.Logf("%-36s median %7.1f ms   worst %7.1f ms", st.name, ms[2], ms[4])
	}
}

// The old way (every card's JSON decoded in Go), to check the new SQL gives the same numbers.
func oldDashboardTotals(t *testing.T, d *sql.DB, acct string) Dashboard {
	var out Dashboard
	rows, err := d.Query(`SELECT data FROM sync_cards WHERE account_id = $1 AND edition = 'sports' AND deleted_at IS NULL
		AND coalesce(status, '') <> 'Sold'`, acct)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		rows.Scan(&raw)
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
	return out
}

func TestLoadSameAnswers(t *testing.T) {
	url := os.Getenv("LOAD_DATABASE_URL")
	if url == "" {
		t.Skip("LOAD_DATABASE_URL not set")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	// Some awkward cards: a pending one, an auction with no bid, text where a number belongs, no sale price.
	var acct string
	d.QueryRow(`SELECT account_id FROM sync_cards GROUP BY 1 ORDER BY 1 LIMIT 1`).Scan(&acct)
	d.Exec(`DELETE FROM sync_cards WHERE data->>'sku' LIKE 'ODD-%'`)
	for i, data := range []string{
		`{"player_name":"(unidentified - pending ID)","status":"New","sku":"ODD-1"}`,
		`{"player_name":"Auction Guy","status":"EBA","start_bid":null,"sale_price":9,"sku":"ODD-2"}`,
		`{"player_name":"Texty","status":"Ready","estimated_value":"12","sale_price":"abc","sku":"ODD-3"}`,
		`{"player_name":"No Price","status":"Ready","sku":"ODD-4","marketplace_listing_id":"x"}`,
		`{"player_name":"Bid Ok","status":"EBA","start_bid":0.99,"sku":"ODD-5"}`,
	} {
		if _, err := d.Exec(`INSERT INTO sync_cards (id, account_id, edition, data, status) VALUES (gen_random_uuid(), $1, 'sports', $2::jsonb, $3)`,
			acct, data, []string{"New", "EBA", "Ready", "Ready", "EBA"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{DB: d}
	got, err := s.dashboard(ctx, acct, "sports", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := oldDashboardTotals(t, d, acct)
	round := func(f float64) string { return fmt.Sprintf("%.2f", f) }
	pairs := [][2]any{
		{got.TotalActiveCards, want.TotalActiveCards}, {got.PricedCount, want.PricedCount},
		{round(got.PricedInventoryValue), round(want.PricedInventoryValue)}, {got.ReadyToListCount, want.ReadyToListCount},
		{round(got.ReadyToListPotentialValue), round(want.ReadyToListPotentialValue)}, {got.LiveCount, want.LiveCount},
		{got.EbayLiveCount, want.EbayLiveCount}, {got.SlabsLiveCount, want.SlabsLiveCount}, {got.BothLiveCount, want.BothLiveCount},
		{got.NeedsIdentificationCount, want.NeedsIdentificationCount}, {got.AiDocumentedCount, want.AiDocumentedCount},
		{got.MissingPricingCount, want.MissingPricingCount}, {got.ListingErrorsCount, want.ListingErrorsCount},
	}
	for i, p := range pairs {
		if fmt.Sprint(p[0]) != fmt.Sprint(p[1]) {
			t.Errorf("dashboard field %d: new %v, old %v", i, p[0], p[1])
		}
	}
	t.Logf("dashboard: %d cards, value %.2f, live %d, errors %d - same as before", got.TotalActiveCards, got.PricedInventoryValue, got.LiveCount, got.ListingErrorsCount)

	// Card list order: the old JSON expressions and the new columns pick the same pages.
	oldSorts := map[string]string{
		"player": `lower(coalesce(c.data->>'player_name', '')) ASC NULLS LAST`,
		"year":   `c.data->'year' DESC NULLS LAST`,
		"value":  `CASE WHEN jsonb_typeof(c.data->'estimated_value') = 'number' THEN (c.data->>'estimated_value')::numeric END DESC NULLS LAST`,
		"added":  `c.data->>'created_at' DESC NULLS LAST`,
		"slot": `lower(split_part(coalesce(b.data->>'name', '~'), ' (cont. ', 1)) ASC,
			coalesce(substring(b.data->>'name' from '\(cont\. (\d+)\)')::int, 1) ASC,
			length(coalesce(c.data->>'slot_row', '')) ASC, c.data->>'slot_row' ASC, c.data->'slot_number' ASC`,
	}
	for sortName, oldOrder := range oldSorts {
		desc := sortName != "player" && sortName != "slot"
		for _, page := range []int{1, 17, 84} {
			pg, err := s.Cards(ctx, acct, Query{Edition: "sports", Sort: sortName, Desc: desc, Page: page, PerPage: 60})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := d.Query(`SELECT c.id FROM sync_cards c
				LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id AND b.deleted_at IS NULL
				WHERE c.account_id = $1 AND c.edition = 'sports' AND c.deleted_at IS NULL AND coalesce(c.status, '') <> 'Sold'
				ORDER BY `+oldOrder+`, c.id LIMIT 60 OFFSET $2`, acct, (page-1)*60)
			if err != nil {
				t.Fatal(err)
			}
			var old []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				old = append(old, id)
			}
			rows.Close()
			var ids []string
			for _, c := range pg.Cards {
				ids = append(ids, c.ID)
			}
			if fmt.Sprint(ids) != fmt.Sprint(old) {
				t.Errorf("sort %s page %d differs from the old order", sortName, page)
			}
		}
	}
	// Search: same matches as the old text match.
	for _, q := range []string{"player 12 prizm", "fcm-41", "silver", "team 3 2019"} {
		pg, err := s.Cards(ctx, acct, Query{Edition: "sports", Search: q, PerPage: 200})
		if err != nil {
			t.Fatal(err)
		}
		where := `c.account_id = $1 AND c.edition = 'sports' AND c.deleted_at IS NULL AND coalesce(c.status, '') <> 'Sold'`
		args := []any{acct}
		for _, w := range Words(q) {
			args = append(args, likeEscape(w))
			where += fmt.Sprintf(` AND concat_ws(' ', c.data->>'player_name', c.data->>'team', c.data->>'brand', c.data->>'set_name',
				c.data->>'card_number', c.data->>'variety', c.data->>'sku', c.data->>'year', c.data->>'serial_number',
				c.data->>'grading_company', c.data->>'card_grade', c.data->>'sport', c.data->>'condition', b.data->>'name') ILIKE $%d`, len(args))
		}
		var n int
		d.QueryRow(`SELECT count(*) FROM sync_cards c LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id
			AND b.deleted_at IS NULL WHERE `+where, args...).Scan(&n)
		if n != pg.Total {
			t.Errorf("search %q: new %d matches, old %d", q, pg.Total, n)
		}
	}
	d.Exec(`DELETE FROM sync_cards WHERE data->>'sku' LIKE 'ODD-%'`)
}

// The Card Editor's sortable headings: each sort runs and puts the right card first.
func TestLoadEditorSorts(t *testing.T) {
	url := os.Getenv("LOAD_DATABASE_URL")
	if url == "" {
		t.Skip("LOAD_DATABASE_URL not set")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	var acct string
	d.QueryRow(`SELECT account_id FROM sync_cards GROUP BY 1 ORDER BY 1 LIMIT 1`).Scan(&acct)
	s := &Service{DB: d}
	var maxSale float64
	d.QueryRow(`SELECT max((data->>'sale_price')::numeric) FROM sync_cards WHERE account_id = $1 AND deleted_at IS NULL
		AND jsonb_typeof(data->'sale_price') = 'number' AND coalesce(status, '') <> 'Sold'`, acct).Scan(&maxSale)
	for _, key := range []string{"sku", "sport", "brand", "number", "price", "status"} {
		for _, desc := range []bool{false, true} {
			start := time.Now()
			pg, err := s.Cards(ctx, acct, Query{Edition: "sports", Sort: key, Desc: desc, PerPage: 50})
			if err != nil || len(pg.Cards) == 0 {
				t.Fatalf("sort %s: %v", key, err)
			}
			if key == "price" && desc {
				var first struct {
					SalePrice float64 `json:"sale_price"`
				}
				json.Unmarshal(pg.Cards[0].Data, &first)
				if first.SalePrice != maxSale {
					t.Fatalf("price desc starts at %v, max is %v", first.SalePrice, maxSale)
				}
			}
			if key == "status" {
				var a, b struct{ Status string }
				json.Unmarshal(pg.Cards[0].Data, &a)
				json.Unmarshal(pg.Cards[len(pg.Cards)-1].Data, &b)
				if (!desc && a.Status > b.Status) || (desc && a.Status < b.Status) {
					t.Fatalf("status order: %s .. %s (desc %v)", a.Status, b.Status, desc)
				}
			}
			t.Logf("sort %-6s desc=%-5v %5.1f ms", key, desc, float64(time.Since(start).Microseconds())/1000)
		}
	}
}
