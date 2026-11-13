package lister

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/ebay"
	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
)

// ErrEbaySetup: eBay isn't connected, or a policy or ship-from location
// hasn't been picked yet (Account > eBay).
type ErrEbaySetup struct{ Msg string }

func (e *ErrEbaySetup) Error() string { return e.Msg }

// EbaySettings are the seller's eBay choices (policies, location, store name).
func (s *Service) EbaySettings(ctx context.Context, accountID string) (ebay.Settings, error) {
	var raw []byte
	var st ebay.Settings
	err := s.DB.QueryRowContext(ctx, `SELECT settings FROM account_market_settings WHERE account_id = $1 AND market = 'ebay'`, accountID).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal(raw, &st)
		return st, nil
	}
	if strings.Contains(err.Error(), "no rows") {
		return st, nil
	}
	return st, err
}

// SaveEbaySettings stores them.
func (s *Service) SaveEbaySettings(ctx context.Context, accountID string, st ebay.Settings) error {
	b, _ := json.Marshal(st)
	return s.SaveSettings(ctx, accountID, "ebay", b)
}

// ebayReady checks eBay can be listed on before a run starts.
func (s *Service) ebayReady(ctx context.Context, accountID string) error {
	if s.Ebay == nil || !s.Ebay.Configured() {
		return &ErrEbaySetup{"eBay isn't switched on for FireCardManager Web yet."}
	}
	if _, err := s.Ebay.AccessToken(ctx, accountID); err != nil {
		if errors.Is(err, ebay.ErrNotConnected) || errors.Is(err, ebay.ErrReconnect) {
			return &ErrEbaySetup{"Connect your eBay account on the Account page first."}
		}
		return err
	}
	st, err := s.EbaySettings(ctx, accountID)
	if err != nil {
		return err
	}
	if m := st.Missing(); len(m) > 0 {
		return &ErrEbaySetup{"Pick your eBay " + strings.Join(m, ", ") + " on the Account page first."}
	}
	return nil
}

func ebayDescribe(err error) string {
	var e *ebay.APIError
	if errors.As(err, &e) {
		if e.Message != "" {
			return "eBay: " + e.Message
		}
		return fmt.Sprintf("eBay error %d", e.Status)
	}
	if errors.Is(err, ebay.ErrReconnect) || errors.Is(err, ebay.ErrNotConnected) {
		return "Connect eBay again on the Account page."
	}
	return "eBay: " + err.Error()
}

// listEbay puts one card on eBay, the way FireCardManager does
// (EbayListingService.PublishAsync): inventory item, offer, publish.
// Photos are the card's hosted FireCardManager Web links; eBay copies them.
func (s *Service) listEbay(ctx context.Context, accountID, edition, cardID string, o slabs.Options) (string, string) {
	token, err := s.Ebay.AccessToken(ctx, accountID)
	if err != nil {
		return "failed", ebayDescribe(err)
	}
	st, err := s.EbaySettings(ctx, accountID)
	if err != nil {
		return "failed", "Couldn't read your eBay settings."
	}
	if m := st.Missing(); len(m) > 0 {
		return "failed", "Pick your eBay " + strings.Join(m, ", ") + " on the Account page."
	}
	profile, ok := st.Profile(o.ShippingProfile)
	if !ok {
		return "failed", "That shipping profile isn't there any more; pick another in the Bulk Lister."
	}
	pol, err := s.ebayPolicies(ctx, accountID, token, profile, st.Returns30)
	if err != nil {
		return "failed", "Couldn't set up eBay shipping for profile \"" + profile.Name + "\": " + ebayDescribe(err)
	}
	var raw []byte
	var version int
	var deleted bool
	err = s.DB.QueryRowContext(ctx, `SELECT data, version, deleted_at IS NOT NULL FROM sync_cards WHERE id = $1 AND account_id = $2`,
		cardID, accountID).Scan(&raw, &version, &deleted)
	if err != nil || deleted {
		return "skipped", "This card is no longer in your inventory."
	}
	var c ebay.Card
	_ = json.Unmarshal(raw, &c)
	var data map[string]json.RawMessage
	_ = json.Unmarshal(raw, &data)
	if c.Status == "Sold" {
		return "skipped", "Already sold."
	}
	photos := []string{}
	for _, side := range []string{"front_photo_id", "back_photo_id"} {
		var id string
		_ = json.Unmarshal(data[side], &id)
		if id != "" && s.PhotoLink != nil {
			photos = append(photos, s.PhotoLink(id))
		}
	}
	if p := ebay.Problems(c, o, len(photos)); len(p) > 0 {
		return "skipped", "Not listed: " + strings.Join(p, ", ") + "."
	}
	var titlePat, descTpl string
	if s.Templates != nil {
		t, err := s.Templates.Get(ctx, accountID, edition, o.Template)
		if err != nil {
			return "failed", "That listing template isn't there any more; pick another in the Bulk Lister."
		}
		if t != nil {
			titlePat, descTpl = t.TitleTemplate, t.DescriptionHTML
		}
	}
	if err := s.Ebay.PutItem(ctx, token, c.Sku, ebay.Item(c, edition, photos, profile, ebay.TitleFrom(titlePat, c, edition))); err != nil {
		return "failed", ebayDescribe(err)
	}
	front, back := photos[0], ""
	if len(photos) > 1 {
		back = photos[1]
	}
	offerID, err := s.Ebay.Offer(ctx, token, ebay.Offer(c, edition, o, st, pol, ebay.DescriptionFrom(descTpl, c, edition, st.StoreName, front, back)))
	if err != nil {
		return "failed", ebayDescribe(err)
	}
	wasLive := c.Status == "EBA" || c.Status == "EBF"
	listingID, err := s.Ebay.Publish(ctx, token, offerID)
	if err != nil {
		return "failed", ebayDescribe(err)
	}

	// Record it on the card; the PC pulls it and shows the card live on eBay.
	status := "EBF"
	if o.Auction {
		status = "EBA"
	}
	j := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	now := time.Now()
	at := j(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
	res, err := s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
		Entity: "card", ID: cardID, BaseVersion: version, EditedAt: now,
		Fields: map[string]json.RawMessage{"status": j(status), "ebay_offer_id": j(offerID), "ebay_listing_id": j(listingID),
			"ebay_listing_url": j("https://www.ebay.com/itm/" + listingID), "updated_at": at},
	}})
	if err != nil || len(res) == 0 || res[0].Status == "rejected" {
		return "failed", "Listed on eBay (" + listingID + "), but the card couldn't be updated here."
	}
	if wasLive {
		return "updated", "Listing updated."
	}
	return "listed", "Listed."
}

// EndEbay ends the cards' eBay listings (the offer is withdrawn, like the
// desktop's Remove from eBay) and puts the cards back to Ready to list.
func (s *Service) EndEbay(ctx context.Context, accountID string, cardIDs []string) (int, []string, error) {
	if s.Ebay == nil {
		return 0, nil, ebay.ErrNotConfigured
	}
	token, err := s.Ebay.AccessToken(ctx, accountID)
	if err != nil {
		return 0, nil, err
	}
	n := 0
	var problems []string
	for _, id := range cardIDs {
		var raw []byte
		var edition string
		var version int
		if err := s.DB.QueryRowContext(ctx, `SELECT data, edition, version FROM sync_cards WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
			strings.ToLower(id), accountID).Scan(&raw, &edition, &version); err != nil {
			continue
		}
		var c ebay.Card
		_ = json.Unmarshal(raw, &c)
		if c.EbayOfferID == nil || *c.EbayOfferID == "" {
			problems = append(problems, c.Sku+": no eBay offer id on this card (end it on eBay or in FireCardManager on your PC).")
			continue
		}
		if err := s.Ebay.Withdraw(ctx, token, *c.EbayOfferID); err != nil {
			problems = append(problems, c.Sku+": "+ebayDescribe(err))
			continue
		}
		now := time.Now()
		at, _ := json.Marshal(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
		if _, err := s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
			Entity: "card", ID: strings.ToLower(id), BaseVersion: version, EditedAt: now,
			Fields: map[string]json.RawMessage{"status": json.RawMessage(`"Ready"`), "ebay_listing_id": json.RawMessage("null"),
				"ebay_listing_url": json.RawMessage("null"), "updated_at": at},
		}}); err != nil {
			return n, problems, err
		}
		n++
	}
	return n, problems, nil
}

// PriceEbay changes a fixed-price eBay listing's price and the card's.
func (s *Service) PriceEbay(ctx context.Context, accountID, cardID string, price float64) error {
	if s.Ebay == nil {
		return ebay.ErrNotConfigured
	}
	token, err := s.Ebay.AccessToken(ctx, accountID)
	if err != nil {
		return err
	}
	var raw []byte
	var edition string
	var version int
	if err := s.DB.QueryRowContext(ctx, `SELECT data, edition, version FROM sync_cards WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		strings.ToLower(cardID), accountID).Scan(&raw, &edition, &version); err != nil {
		return err
	}
	var c ebay.Card
	_ = json.Unmarshal(raw, &c)
	if c.EbayOfferID == nil || *c.EbayOfferID == "" || c.Status != "EBF" {
		return &PriceError{"Only a Buy It Now listing made with an offer id can be repriced here."}
	}
	if err := s.Ebay.SetPrice(ctx, token, c.Sku, *c.EbayOfferID, price); err != nil {
		return &PriceError{ebayDescribe(err)}
	}
	now := time.Now()
	p, _ := json.Marshal(float64(int(price*100+0.5)) / 100)
	at, _ := json.Marshal(now.UTC().Format("2006-01-02T15:04:05.0000000Z"))
	_, err = s.Sync.Push(ctx, cloudsync.Device{AccountID: accountID, Edition: edition}, []cloudsync.Change{{
		Entity: "card", ID: strings.ToLower(cardID), BaseVersion: version, EditedAt: now,
		Fields: map[string]json.RawMessage{"sale_price": p, "updated_at": at},
	}})
	return err
}

type polKey struct {
	account, profile string
	returns          bool
	shipping         string // the profile's policy name parts, so an edited profile gets a new policy
}

type polEntry struct {
	pol ebay.Policies
	at  time.Time
}

// ebayPolicies finds or makes the run's eBay policies; remembered for 15
// minutes so a run of many cards checks eBay once.
func (s *Service) ebayPolicies(ctx context.Context, accountID, token string, p ebay.Profile, returns30 bool) (ebay.Policies, error) {
	k := polKey{accountID, p.ID, returns30, fmt.Sprintf("%s|%s|%s|%.2f|%.2f|%d", p.Name, p.ServiceCode, p.Cost, p.FlatAmount, p.WeightOz, p.HandlingDays)}
	if v, ok := s.polCache.Load(k); ok && time.Since(v.(polEntry).at) < 15*time.Minute {
		return v.(polEntry).pol, nil
	}
	ship, pay, ret, err := s.Ebay.ResolvePolicies(ctx, token, p, returns30)
	if err != nil {
		return ebay.Policies{}, err
	}
	pol := ebay.Policies{Shipping: ship, Payment: pay, Return: ret}
	s.polCache.Store(k, polEntry{pol, time.Now()})
	return pol, nil
}
