package lister

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
)

// LiveCard is a card listed somewhere, for the Live Listings page.
type LiveCard struct {
	ID      string          `json:"id"`
	BoxName string          `json:"box_name,omitempty"`
	Data    json.RawMessage `json:"data"`
	// From Slabs & Singles, when the seller's web key is set.
	Slabs *slabs.SyncItem `json:"slabs,omitempty"`
}

// LiveResult is the page: the cards, and whether Slabs & Singles was checked.
type LiveResult struct {
	Cards        []LiveCard `json:"cards"`
	SlabsChecked bool       `json:"slabs_checked"`
	SlabsProblem string     `json:"slabs_problem,omitempty"`
}

// Live lists the edition's cards that are live on eBay or Slabs & Singles,
// with each Slabs & Singles listing's status and price as the site has them.
func (s *Service) Live(ctx context.Context, accountID, edition string) (LiveResult, error) {
	out := LiveResult{Cards: []LiveCard{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, coalesce(b.data->>'name', ''), c.data FROM sync_cards c
		LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id AND b.deleted_at IS NULL
		WHERE c.account_id = $1 AND c.edition = $2 AND c.deleted_at IS NULL AND coalesce(c.status, '') <> 'Sold'
		  AND (c.status IN ('EBA', 'EBF') OR coalesce(c.data->>'marketplace_listing_id', '') <> '')
		ORDER BY c.data->>'marketplace_listed_at' DESC NULLS LAST, c.id`, accountID, edition)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c LiveCard
		var data []byte
		if err := rows.Scan(&c.ID, &c.BoxName, &data); err != nil {
			return out, err
		}
		c.Data = data
		out.Cards = append(out.Cards, c)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	key, _, err := s.key(ctx, accountID, "slabs")
	if err != nil {
		return out, nil // no web key: the page still shows what the PC synced
	}
	items, err := s.Slabs.Listings(ctx, key)
	if err != nil {
		out.SlabsProblem = describe(err)
		return out, nil
	}
	bySku := map[string]*slabs.SyncItem{}
	for i := range items {
		bySku[items[i].ExternalID] = &items[i]
	}
	out.SlabsChecked = true
	for i := range out.Cards {
		var c slabs.Card
		_ = json.Unmarshal(out.Cards[i].Data, &c)
		out.Cards[i].Slabs = bySku[c.Sku]
	}
	return out, nil
}

func (s *Service) liveCard(ctx context.Context, accountID, cardID string) (slabs.Card, string, int, error) {
	var raw []byte
	var edition string
	var version int
	err := s.DB.QueryRowContext(ctx, `SELECT data, edition, version FROM sync_cards WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		strings.ToLower(cardID), accountID).Scan(&raw, &edition, &version)
	var c slabs.Card
	_ = json.Unmarshal(raw, &c)
	return c, edition, version, err
}

// EndSlabs takes the cards' Slabs & Singles listings down and marks the
// cards not live there (the PC pulls that).
func (s *Service) EndSlabs(ctx context.Context, accountID string, cardIDs []string) (int, []string, error) {
	key, _, err := s.key(ctx, accountID, "slabs")
	if err != nil {
		return 0, nil, err
	}
	n := 0
	var problems []string
	for _, id := range cardIDs {
		c, edition, version, err := s.liveCard(ctx, accountID, id)
		if err != nil || c.Sku == "" {
			continue
		}
		if err := s.Slabs.End(ctx, key, c.Sku); err != nil {
			problems = append(problems, c.Sku+": "+describe(err))
			continue
		}
		now := time.Now()
		at, _ := json.Marshal(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
		if _, err := s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
			Entity: "card", ID: strings.ToLower(id), BaseVersion: version, EditedAt: now,
			Fields: map[string]json.RawMessage{"marketplace_listing_id": json.RawMessage("null"), "marketplace_listed_at": json.RawMessage("null"), "updated_at": at},
		}}); err != nil {
			return n, problems, err
		}
		n++
	}
	return n, problems, nil
}

// PriceSlabs changes a card's price on Slabs & Singles and on the card.
func (s *Service) PriceSlabs(ctx context.Context, accountID, cardID string, price float64) error {
	key, _, err := s.key(ctx, accountID, "slabs")
	if err != nil {
		return err
	}
	c, edition, version, err := s.liveCard(ctx, accountID, cardID)
	if err != nil {
		return err
	}
	if err := s.Slabs.SetPrice(ctx, key, c.Sku, int(math.Round(price*100))); err != nil {
		return &PriceError{describe(err)}
	}
	now := time.Now()
	p, _ := json.Marshal(math.Round(price*100) / 100)
	at, _ := json.Marshal(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
	_, err = s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
		Entity: "card", ID: strings.ToLower(cardID), BaseVersion: version, EditedAt: now,
		// marketplace_listed_at moves with it, so the PC takes the site's price as the newer one.
		Fields: map[string]json.RawMessage{"sale_price": p, "marketplace_listed_at": at, "updated_at": at},
	}})
	return err
}

// PriceError is Slabs & Singles turning a price change down.
type PriceError struct{ Msg string }

func (e *PriceError) Error() string { return e.Msg }
