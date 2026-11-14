package ebay

import (
	"testing"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
)

func f(v float64) *float64 { return &v }

func TestOfferRunChoices(t *testing.T) {
	c := Card{Card: slabs.Card{Sku: "FCM-1", PlayerName: "Goku", SetName: "Saiyan Saga", Sport: "Baseball"}}
	at := time.Now().Add(48 * time.Hour)
	o := slabs.Options{FallbackPrice: f(4), AllowOffers: true, OfferDollars: true, AcceptAmount: f(3.5), DeclineAmount: f(2), CategoryID: "183454", StartAt: &at}
	if p := Problems(c, o, 1); len(p) != 0 {
		t.Fatalf("problems %v", p)
	}
	off := Offer(c, "tcg", o, Settings{LocationKey: "x"}, Policies{}, "d")
	if off["categoryId"] != "183454" || off["listingStartDate"] == nil {
		t.Fatalf("offer %v", off)
	}
	if got := off["pricingSummary"].(map[string]any)["price"].(map[string]string)["value"]; got != "4.00" {
		t.Fatalf("price %s", got)
	}
	terms := off["listingPolicies"].(map[string]any)["bestOfferTerms"].(map[string]any)
	if terms["autoAcceptPrice"].(map[string]string)["value"] != "3.50" || terms["autoDeclinePrice"].(map[string]string)["value"] != "2.00" {
		t.Fatalf("terms %v", terms)
	}
	// Auction: the run's reserve when the card has none.
	o = slabs.Options{Auction: true, DurationDays: 3, DefaultStartBid: f(1), ReservePrice: f(5)}
	off = Offer(c, "tcg", o, Settings{}, Policies{}, "d")
	ps := off["pricingSummary"].(map[string]any)
	if ps["auctionReservePrice"].(map[string]string)["value"] != "5.00" || off["listingDuration"] != "DAYS_3" {
		t.Fatalf("auction %v", off)
	}
	if r := (Settings{}).Returns("30D-FreeReturns"); !r.Accepted || r.Days != 30 || r.Payer != "SELLER" || r.Name != "30D-FreeReturns" {
		t.Fatalf("returns %+v", r)
	}
	if r := (Settings{Returns30: true}).Returns(""); r.Name != "30 Day Returns" {
		t.Fatalf("legacy returns %+v", r)
	}
}
