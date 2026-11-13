package ebay

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strings"

	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
)

// Card is a synced card's fields an eBay listing needs (the desktop's names).
type Card struct {
	slabs.Card
	EstimatedValue *float64 `json:"estimated_value"`
	IsSketchCard   bool     `json:"is_sketch_card"`
	SlotRow        *string  `json:"slot_row"`
	SlotNumber     *int     `json:"slot_number"`
	EbayOfferID    *string  `json:"ebay_offer_id"`
	EbayListingID  *string  `json:"ebay_listing_id"`
}

// Settings are the seller's eBay choices on the website (Account > eBay):
// shipping profiles like FireCardManager's, returns, ship-from location and
// store name. eBay shipping/payment/return policies are made from these
// when listing, the way the desktop does it.
type Settings struct {
	Profiles    []Profile `json:"profiles,omitempty"`
	Returns30   bool      `json:"returns_30"`
	LocationKey string    `json:"location_key"`
	StoreName   string    `json:"store_name"`
}

// ProfileList is the seller's profiles, or the starter three.
func (s Settings) ProfileList() []Profile {
	if len(s.Profiles) > 0 {
		return s.Profiles
	}
	return DefaultProfiles()
}

// Profile picks one by id ("" picks the first).
func (s Settings) Profile(id string) (Profile, bool) {
	list := s.ProfileList()
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	if id == "" && len(list) > 0 {
		return list[0], true
	}
	return Profile{}, false
}

// Missing says what's still needed before listing.
func (s Settings) Missing() []string {
	if s.LocationKey == "" {
		return []string{"ship-from location"}
	}
	return nil
}

// Policies are the eBay policy ids one listing uses.
type Policies struct{ Shipping, Payment, Return string }

var (
	leadingYear = regexp.MustCompile(`^\s*(19|20)\d{2}(\D|$)`)
	spaces      = regexp.MustCompile(`\s{2,}`)
)

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func yearLabel(c Card) string {
	if c.Year <= 0 {
		return ""
	}
	if c.SeasonEnd != nil && *c.SeasonEnd != c.Year {
		return fmt.Sprintf("%d-%02d", c.Year, *c.SeasonEnd%100)
	}
	return fmt.Sprint(c.Year)
}

// TCG games eBay tells apart by its "Game" item specific (SportInfo.EbayGame).
var games = map[string]string{
	"Pokemon": "Pokémon TCG", "Magic": "Magic: The Gathering", "YuGiOh": "Yu-Gi-Oh! TCG", "OnePiece": "One Piece CCG",
	"Lorcana": "Disney Lorcana TCG", "DragonBall": "Dragon Ball Super Card Game", "Digimon": "Digimon CCG",
	"FleshAndBlood": "Flesh and Blood TCG", "FinalFantasy": "Final Fantasy TCG", "StarWarsTcg": "Star Wars TCG",
	"WeissSchwarz": "Weiss Schwarz", "Vanguard": "Cardfight!! Vanguard TCG", "Naruto": "Naruto CCG", "MetaZoo": "MetaZoo CCG",
	"Riftbound": "Riftbound: League of Legends TCG", "WorldOfWarcraft": "World of Warcraft TCG", "MyLittlePony": "My Little Pony CCG",
	"Sorcery": "Sorcery: Contested Realm", "Netrunner": "Netrunner", "UniVersus": "UniVersus",
}

var sportNames = map[string]string{"Baseball": "Baseball", "Basketball": "Basketball", "Boxing": "Boxing", "Football": "Football", "Golf": "Golf",
	"Hockey": "Hockey", "Mma": "MMA", "Multisport": "Multi-Sport", "Racing": "Racing", "Soccer": "Soccer", "Tennis": "Tennis", "Wrestling": "Wrestling"}

// CategoryID is eBay's category for the card (EbayCategorySuggestions).
func CategoryID(c Card, edition string) string {
	if edition == "tcg" {
		if _, ok := games[c.Sport]; ok || c.Sport == "OtherTcg" {
			return "183454" // CCG Individual Cards
		}
		return "183050" // Non-Sport Trading Card Singles
	}
	switch c.Sport {
	case "Basketball":
		return "214"
	case "Football":
		return "215"
	}
	return "213" // Baseball, and the starting point for the rest (the desktop does the same)
}

// DefaultTitle is the Store Branded template's title.
const DefaultTitle = "{Year} {Brand} {SetName} {PlayerName} #{CardNumber} {Variety} {Condition}"

func render(tpl string, c Card, edition, store, front, back string, title bool) string {
	year, brand := yearLabel(c), c.Brand
	if title && leadingYear.MatchString(c.SetName) {
		year = ""
	}
	if title && brand != "" && strings.Contains(strings.ToLower(c.SetName), strings.ToLower(strings.TrimSpace(brand))) {
		brand = ""
	}
	if store == "" {
		store = "Your Store Name"
	}
	est := ""
	if c.EstimatedValue != nil {
		est = fmt.Sprintf("$%.2f", *c.EstimatedValue)
	}
	esc := func(s string) string {
		if title {
			return s
		}
		return html.EscapeString(s)
	}
	r := strings.NewReplacer("{StoreName}", esc(store), "{PlayerName}", esc(c.PlayerName), "{Team}", esc(str(c.Team)), "{Year}", year,
		"{Brand}", esc(brand), "{SetName}", esc(c.SetName), "{CardNumber}", esc(c.CardNumber), "{Variety}", esc(str(c.Variety)),
		"{Condition}", esc(c.Condition), "{Sport}", esc(c.Sport), "{EstimatedValue}", est, "{BoxLabel}", "",
		"{FrontImageUrl}", front, "{BackImageUrl}", back, "{CardKind}", map[bool]string{true: "Trading Cards", false: "Sports Cards"}[edition == "tcg"],
		"{SportLabel}", map[bool]string{true: "Category", false: "Sport"}[edition == "tcg"], "{TeamLabel}", map[bool]string{true: "Franchise", false: "Team"}[edition == "tcg"])
	out := r.Replace(tpl)
	if title {
		out = strings.TrimSpace(spaces.ReplaceAllString(strings.ReplaceAll(out, " # ", " "), " "))
		out = strings.TrimSuffix(out, " #")
		if rs := []rune(out); len(rs) > 80 {
			out = strings.TrimSpace(string(rs[:80]))
		}
	}
	return out
}

// Title is the listing title from the Store Branded template.
func Title(c Card, edition string) string { return render(DefaultTitle, c, edition, "", "", "", true) }

// TitleFrom fills in a template's title pattern ("" uses the default).
func TitleFrom(pattern string, c Card, edition string) string {
	if strings.TrimSpace(pattern) == "" {
		pattern = DefaultTitle
	}
	return render(pattern, c, edition, "", "", "", true)
}

// DescriptionFrom fills in a template's description ("" uses Store Branded).
func DescriptionFrom(tpl string, c Card, edition, store, front, back string) string {
	if strings.TrimSpace(tpl) == "" {
		tpl = DefaultDescription
	}
	return render(tpl, c, edition, store, front, back, false) + footer
}

const footer = `<p style="margin-top:16px;color:#666;font-size:12px;">Provided by FireCardManager.com</p>`

// Description is the Store Branded template's HTML with the card filled in.
func Description(c Card, edition, store, front, back string) string {
	return render(DefaultDescription, c, edition, store, front, back, false) + footer
}

func conditionID(cond string) string {
	c := strings.ToUpper(strings.TrimSpace(cond))
	switch {
	case strings.Contains(c, "MINT") || strings.Contains(c, "NM") || strings.Contains(c, "GEM"):
		return "400010" // Near Mint or Better
	case strings.Contains(c, "EXCELLENT") || c == "EX" || strings.HasPrefix(c, "EX-"):
		return "400011"
	case strings.Contains(c, "POOR") || strings.Contains(c, "FAIR") || strings.Contains(c, "DAMAGE"):
		return "400013"
	}
	return "400012" // Very Good
}

func graded(c Card) bool { g := str(c.GradingCompany); return g != "" && !strings.EqualFold(g, "raw") }

// Item is the inventory item for the card (EbayListingService.PublishAsync).
func Item(c Card, edition string, photos []string, pkg Profile, title string) map[string]any {
	a := map[string][]string{}
	set := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			a[k] = []string{strings.TrimSpace(v)}
		}
	}
	if edition == "tcg" {
		if g, ok := games[c.Sport]; ok {
			a["Game"] = []string{g}
			set("Card Name", c.PlayerName)
		} else if c.Sport == "OtherTcg" {
			set("Game", str(c.Team))
			set("Card Name", c.PlayerName)
		} else {
			set("Franchise", str(c.Team))
			set("Character", c.PlayerName)
		}
		set("Set", c.SetName)
		if c.Year > 0 {
			set("Year Manufactured", fmt.Sprint(c.Year))
		}
		set("Manufacturer", c.Brand)
		set("Parallel/Variety", str(c.Variety))
		set("Card Number", c.CardNumber)
		var f []string
		for _, x := range []struct {
			on   bool
			name string
		}{{c.IsAutograph, "Autograph"}, {c.IsMemorabilia, "Memorabilia/Relic"}, {c.IsShortPrint, "Short Print (SP)"}, {c.IsChase, "Chase"},
			{c.IsFoil, "Foil"}, {c.IsSketchCard, "Sketch Card"}, {c.IsErrorVariant, "Error/Variant"},
			{strings.Contains(strings.ToLower(str(c.Variety)), "refractor"), "Refractor"}} {
			if x.on {
				f = append(f, x.name)
			}
		}
		if len(f) > 0 {
			a["Features"] = f
		}
		a["Graded"] = []string{map[bool]string{true: "Yes", false: "No"}[graded(c)]}
		if graded(c) {
			set("Professional Grader", str(c.GradingCompany))
		}
	} else {
		set("Sport", sportNames[c.Sport])
		set("Player/Athlete", c.PlayerName)
		set("Set", c.SetName)
		set("Season", yearLabel(c))
		set("Manufacturer", c.Brand)
		set("Parallel/Variety", str(c.Variety))
		set("Team", str(c.Team))
		set("Card Number", c.CardNumber)
	}
	a["Autographed"] = []string{map[bool]string{true: "Yes", false: "No"}[c.IsAutograph]}

	item := map[string]any{
		"availability": map[string]any{"shipToLocationAvailability": map[string]int{"quantity": 1}},
		"product":      map[string]any{"title": title, "imageUrls": photos, "aspects": a},
	}
	if pkg.WeightOz > 0 {
		item["packageWeightAndSize"] = pkg.PackageDetails()
	}
	// eBay's trading-card categories take only these two conditions; an
	// ungraded card also needs its Card Condition (descriptor 40001).
	if graded(c) {
		item["condition"] = "LIKE_NEW"
	} else {
		item["condition"] = "USED_VERY_GOOD"
		item["conditionDescriptors"] = []map[string]any{{"name": "40001", "values": []string{conditionID(c.Condition)}}}
	}
	return item
}

func money(v float64) map[string]string {
	return map[string]string{"value": fmt.Sprintf("%.2f", v), "currency": "USD"}
}

// Problems are why the card can't go on eBay yet.
func Problems(c Card, o slabs.Options, photos int) []string {
	var p []string
	if c.Sku == "" {
		p = append(p, "no SKU yet")
	}
	if photos == 0 {
		p = append(p, "no photo")
	}
	if c.PlayerName == "" || strings.HasPrefix(c.PlayerName, "(unidentified") {
		p = append(p, "not identified")
	}
	if !o.Auction && (c.SalePrice == nil || *c.SalePrice < 0.99) {
		p = append(p, "no price")
	}
	if o.Auction {
		if (c.StartBid == nil || *c.StartBid <= 0) && (o.DefaultStartBid == nil || *o.DefaultStartBid <= 0) {
			p = append(p, "no starting bid")
		}
	}
	return p
}

// Offer is the offer for the card's inventory item.
func Offer(c Card, edition string, o slabs.Options, st Settings, pol Policies, description string) map[string]any {
	price := 0.0
	if c.SalePrice != nil {
		price = *c.SalePrice
	}
	policies := map[string]any{"fulfillmentPolicyId": pol.Shipping, "paymentPolicyId": pol.Payment, "returnPolicyId": pol.Return}
	pricing := map[string]any{}
	offer := map[string]any{
		"sku": c.Sku, "marketplaceId": Marketplace, "availableQuantity": 1, "categoryId": CategoryID(c, edition),
		"listingDescription": description, "merchantLocationKey": st.LocationKey, "listingPolicies": policies, "pricingSummary": pricing,
	}
	if o.Auction {
		start := 0.0
		if c.StartBid != nil && *c.StartBid > 0 {
			start = *c.StartBid
		} else if o.DefaultStartBid != nil {
			start = *o.DefaultStartBid
		}
		days := o.DurationDays
		if days != 1 && days != 3 && days != 5 && days != 10 {
			days = 7
		}
		offer["format"], offer["listingDuration"] = "AUCTION", fmt.Sprintf("DAYS_%d", days)
		pricing["price"], pricing["auctionStartPrice"] = money(start), money(start)
		if c.ReservePrice != nil && *c.ReservePrice > start {
			pricing["auctionReservePrice"] = money(*c.ReservePrice)
		}
		return offer
	}
	offer["format"] = "FIXED_PRICE"
	pricing["price"] = money(price)
	if o.AllowOffers {
		terms := map[string]any{"bestOfferEnabled": true}
		if o.AcceptPercent != nil && *o.AcceptPercent > 0 {
			terms["autoAcceptPrice"] = money(math.Round(price*float64(*o.AcceptPercent)) / 100)
		}
		if o.DeclinePercent != nil && *o.DeclinePercent > 0 {
			terms["autoDeclinePrice"] = money(math.Round(price*float64(*o.DeclinePercent)) / 100)
		}
		policies["bestOfferTerms"] = terms
	}
	return offer
}
