// Package slabs lists cards on Slabs & Singles with the seller's own API key,
// the same way FireCardManager does (SlabsClient / SlabsListingMapper):
// photos are uploaded to the marketplace, then the listing is created or
// replaced by the card's SKU.
package slabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://slabsnsingles.com/api"

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("Slabs & Singles returned %d", e.Status)
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) send(ctx context.Context, key, method, path, contentType string, body []byte, idem string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "FireCardManager-Web")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Slabs & Singles: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string            `json:"code"`
				Message string            `json:"message"`
				Fields  map[string]string `json:"fields"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		msg := e.Error.Message
		for k, v := range e.Error.Fields {
			msg += fmt.Sprintf(" (%s: %s)", k, v)
		}
		return &APIError{Status: res.StatusCode, Code: e.Error.Code, Message: strings.TrimSpace(msg)}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// UploadPhoto puts a JPEG on the marketplace's photo hosting; returns its URL.
func (c *Client) UploadPhoto(ctx context.Context, key string, jpeg []byte) (string, error) {
	var out struct {
		Photo struct {
			URL string `json:"url"`
		} `json:"photo"`
	}
	if err := c.send(ctx, key, http.MethodPost, "/v1/photos", "image/jpeg", jpeg, "", &out); err != nil {
		return "", err
	}
	if out.Photo.URL == "" {
		return "", &APIError{Message: "The photo upload didn't return a link."}
	}
	return out.Photo.URL, nil
}

type Listing struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
}

// Upsert creates the listing, or replaces the one with this SKU.
func (c *Client) Upsert(ctx context.Context, key string, in Input, idem string) (Listing, bool, error) {
	body, _ := json.Marshal(in)
	var out struct {
		Listing Listing `json:"listing"`
		Created bool    `json:"created"`
	}
	err := c.send(ctx, key, http.MethodPost, "/v1/listings", "application/json", body, idem, &out)
	return out.Listing, out.Created, err
}

// ---------------- the listing (SlabsListingInput) ----------------

type Offers struct {
	Enabled      bool    `json:"enabled"`
	AcceptKind   *string `json:"accept_kind,omitempty"`
	AcceptValue  *int    `json:"accept_value,omitempty"`
	DeclineKind  *string `json:"decline_kind,omitempty"`
	DeclineValue *int    `json:"decline_value,omitempty"`
}

type Auction struct {
	StartCents   int  `json:"start_cents"`
	ReserveCents *int `json:"reserve_cents,omitempty"`
	BuyNowCents  *int `json:"buy_now_cents,omitempty"`
	DurationDays int  `json:"duration_days"`
}

type Input struct {
	ExternalID    string   `json:"external_id"`
	Category      string   `json:"category"`
	GameOrSport   string   `json:"game_or_sport"`
	SetName       string   `json:"set_name"`
	Year          *int     `json:"year,omitempty"`
	CardNumber    *string  `json:"card_number,omitempty"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Condition     *string  `json:"condition,omitempty"`
	Grader        *string  `json:"grader,omitempty"`
	Grade         *float64 `json:"grade,omitempty"`
	CertNumber    *string  `json:"cert_number,omitempty"`
	Attributes    []string `json:"attributes"`
	PhotoURLs     []string `json:"photo_urls"`
	PriceCents    int      `json:"price_cents"`
	Quantity      int      `json:"quantity"`
	PackagePreset string   `json:"package_preset"`
	WeightOz      float64  `json:"weight_oz"`
	Format        string   `json:"format"`
	Auction       *Auction `json:"auction,omitempty"`
	Offers        *Offers  `json:"offers,omitempty"`
}

// Options are the Bulk Lister's choices (the app's SlabsListingOptions).
type Options struct {
	Auction             bool     `json:"auction"`
	DurationDays        int      `json:"duration_days"`
	DefaultStartBid     *float64 `json:"default_start_bid"`
	BuyItNowAtSalePrice bool     `json:"buy_it_now_at_sale_price"`
	AllowOffers         bool     `json:"allow_offers"`
	AcceptPercent       *int     `json:"accept_percent"`
	DeclinePercent      *int     `json:"decline_percent"`
}

// Ship are the seller's package defaults from FireCardManager's settings.
type Ship struct {
	PackageMode    string  `json:"package_mode"` // auto, bubble, box
	RawWeightOz    float64 `json:"raw_weight_oz"`
	GradedWeightOz float64 `json:"graded_weight_oz"`
}

// Card is the card's synced fields that a listing needs.
type Card struct {
	Sku            string   `json:"sku"`
	Sport          string   `json:"sport"`
	PlayerName     string   `json:"player_name"`
	Team           *string  `json:"team"`
	Year           int      `json:"year"`
	SeasonEnd      *int     `json:"season_end"`
	Brand          string   `json:"brand"`
	SetName        string   `json:"set_name"`
	CardNumber     string   `json:"card_number"`
	Variety        *string  `json:"variety"`
	SerialNumber   *string  `json:"serial_number"`
	Condition      string   `json:"condition"`
	GradingCompany *string  `json:"grading_company"`
	CardGrade      *string  `json:"card_grade"`
	CertNumber     *string  `json:"grading_cert_number"`
	SalePrice      *float64 `json:"sale_price"`
	StartBid       *float64 `json:"start_bid"`
	ReservePrice   *float64 `json:"reserve_price"`
	Status         string   `json:"status"`
	AllowOffers    bool     `json:"allow_offers"`
	AcceptPercent  *int     `json:"accept_offer_percent"`
	RejectPercent  *int     `json:"reject_offer_percent"`
	IsRookie       bool     `json:"is_rookie"`
	IsAutograph    bool     `json:"is_autograph"`
	IsMemorabilia  bool     `json:"is_memorabilia"`
	IsShortPrint   bool     `json:"is_short_print"`
	IsErrorVariant bool     `json:"is_error_variant"`
	IsFoil         bool     `json:"is_foil"`
	IsChase        bool     `json:"is_chase"`
}

func s(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

var sportNames = map[string]string{"Baseball": "Baseball", "Basketball": "Basketball", "Football": "Football", "Hockey": "Hockey",
	"Soccer": "Soccer", "Wrestling": "Wrestling", "Racing": "Auto Racing", "Boxing": "Boxing", "Golf": "Golf", "Mma": "MMA", "Tennis": "Tennis"}

var tcgPlacement = map[string][2]string{
	"Entertainment": {"nonsport", "Entertainment"}, "Superheroes": {"nonsport", "Superheroes"}, "SciFi": {"nonsport", "Sci-Fi"},
	"Fantasy": {"nonsport", "Fantasy"}, "Anime": {"nonsport", "Anime"}, "Horror": {"nonsport", "Horror"}, "Music": {"nonsport", "Music"},
	"Historical": {"nonsport", "Historical"}, "Novelty": {"nonsport", "Novelty"}, "Mixed": {"nonsport", "Other non-sport"},
	"Pokemon": {"tcg", "Pokemon"}, "Magic": {"tcg", "Magic"}, "YuGiOh": {"tcg", "Yu-Gi-Oh!"}, "OnePiece": {"tcg", "One Piece"},
	"Lorcana": {"tcg", "Lorcana"}, "DragonBall": {"tcg", "Dragon Ball"}, "Digimon": {"tcg", "Digimon"},
	"FleshAndBlood": {"tcg", "Flesh and Blood"}, "FinalFantasy": {"tcg", "Final Fantasy"}, "StarWarsTcg": {"tcg", "Star Wars"},
	"WeissSchwarz": {"tcg", "Weiss Schwarz"}, "Vanguard": {"tcg", "Cardfight!! Vanguard"}, "Naruto": {"tcg", "Naruto"},
	"MetaZoo": {"tcg", "MetaZoo"}, "Riftbound": {"tcg", "Riftbound"}, "WorldOfWarcraft": {"tcg", "World of Warcraft"},
	"MyLittlePony": {"tcg", "My Little Pony"}, "Sorcery": {"tcg", "Sorcery"}, "Netrunner": {"tcg", "Netrunner"},
	"UniVersus": {"tcg", "UniVersus"}, "OtherTcg": {"tcg", "Other TCG"},
}

// placement is the marketplace category and game/sport for the card.
func placement(c Card, edition string) (string, string, bool) {
	if edition == "tcg" {
		p, ok := tcgPlacement[c.Sport]
		return p[0], p[1], ok
	}
	n, ok := sportNames[c.Sport]
	return "sports", n, ok
}

var (
	reHP   = regexp.MustCompile(`\bHP\b`)
	reGood = regexp.MustCompile(`\bGOOD\b`)
	reVG   = regexp.MustCompile(`\bVG\b|\bMP\b`)
	reLP   = regexp.MustCompile(`\bEX\b|\bLP\b`)
	reNM   = regexp.MustCompile(`\bNM\b`)
	reGrd  = regexp.MustCompile(`(10|[1-9](\.5)?)(\D|$)`)
	reYear = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

// MapCondition: the marketplace's raw grades NM, LP, MP, HP, DMG.
func MapCondition(cond string) string {
	c := strings.ToUpper(strings.TrimSpace(cond))
	switch {
	case strings.Contains(c, "DAMAGE") || strings.Contains(c, "POOR") || strings.Contains(c, "CREASE"):
		return "DMG"
	case strings.Contains(c, "FAIR") || strings.Contains(c, "HEAVY") || reHP.MatchString(c) || (reGood.MatchString(c) && !strings.Contains(c, "VERY")):
		return "HP"
	case strings.Contains(c, "VERY GOOD") || reVG.MatchString(c) || strings.Contains(c, "MODERATE"):
		return "MP"
	case strings.Contains(c, "EXCELLENT") || reLP.MatchString(c) || strings.HasPrefix(c, "EX-") || strings.Contains(c, "LIGHT"):
		return "LP"
	case strings.Contains(c, "MINT") || reNM.MatchString(c) || strings.Contains(c, "GEM"):
		return "NM"
	}
	return "LP"
}

func mapGrader(company string) string {
	c := strings.ToUpper(strings.TrimSpace(company))
	if c == "" {
		return ""
	}
	if strings.Contains(c, "BECKETT") {
		return "BGS"
	}
	for _, g := range []string{"PSA", "BGS", "SGC", "CGC", "TAG"} {
		if strings.Contains(c, g) {
			return g
		}
	}
	return "OTHER"
}

func parseGrade(g string) *float64 {
	m := reGrd.FindStringSubmatch(g)
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	return &v
}

func cents(f *float64) *int {
	if f == nil || *f <= 0 {
		return nil
	}
	v := int(math.Round(*f * 100))
	return &v
}

func yearLabel(c Card) string {
	if c.SeasonEnd != nil && *c.SeasonEnd != c.Year {
		return fmt.Sprintf("%d-%02d", c.Year, *c.SeasonEnd%100)
	}
	return strconv.Itoa(c.Year)
}

// Title as the app builds it.
func Title(c Card) string {
	var p []string
	if c.Year > 0 && !reYear.MatchString(c.SetName) {
		p = append(p, yearLabel(c))
	}
	if b := strings.TrimSpace(c.Brand); b != "" && !strings.Contains(strings.ToLower(c.SetName), strings.ToLower(b)) {
		p = append(p, b)
	}
	for _, v := range []string{c.SetName, c.PlayerName, s(c.Variety)} {
		if v = strings.TrimSpace(v); v != "" {
			p = append(p, v)
		}
	}
	if n := strings.TrimLeft(strings.TrimSpace(c.CardNumber), "#"); n != "" {
		p = append(p, "#"+n)
	}
	if sn := s(c.SerialNumber); sn != "" {
		if strings.Contains(sn, "/") {
			p = append(p, sn)
		} else {
			p = append(p, "#'d "+sn)
		}
	}
	if c.IsRookie {
		p = append(p, "RC")
	}
	if c.IsAutograph {
		p = append(p, "Auto")
	}
	t := strings.Join(p, " ")
	if r := []rune(t); len(r) > 200 {
		t = string(r[:200])
	}
	return t
}

func priceCents(c Card) *int {
	if c.SalePrice != nil && *c.SalePrice > 0 {
		return cents(c.SalePrice)
	}
	if c.Status == "EBA" {
		return cents(c.StartBid)
	}
	return nil
}

func startCents(c Card, o Options) *int {
	if v := cents(c.StartBid); v != nil {
		return v
	}
	return cents(o.DefaultStartBid)
}

// Problems are why a card can't be listed yet (the app's wording).
func Problems(c Card, edition string, hasPhotos bool, o Options) []string {
	var p []string
	if o.Auction && startCents(c, o) == nil {
		p = append(p, "set a Start Bid (or a default starting bid)")
	}
	if _, _, ok := placement(c, edition); !ok {
		if edition == "tcg" {
			p = append(p, fmt.Sprintf("set a category (not %s)", orNone(c.Sport)))
		} else {
			p = append(p, fmt.Sprintf("set a specific sport (not %s)", orNone(c.Sport)))
		}
	}
	if !o.Auction && priceCents(c) == nil {
		p = append(p, "set a Sale Price")
	}
	if !hasPhotos {
		p = append(p, "add a front photo")
	}
	if strings.TrimSpace(c.PlayerName) == "" || strings.HasPrefix(c.PlayerName, "(unidentified") {
		p = append(p, "identify the card first")
	}
	if strings.TrimSpace(c.SetName) == "" {
		p = append(p, "fill in the set")
	}
	return p
}

func orNone(v string) string {
	if v == "" {
		return "blank"
	}
	return v
}

func ptr[T any](v T) *T { return &v }

// Build is the listing for a card (SlabsListingMapper.ToInput).
func Build(c Card, edition string, photos []string, o Options, ship Ship) Input {
	grader := mapGrader(s(c.GradingCompany))
	var grade *float64
	if grader != "" {
		grade = parseGrade(s(c.CardGrade))
		if grade == nil {
			grade = parseGrade(c.Condition)
		}
		if grade == nil {
			grader = "" // a slab with no readable grade lists as raw
		}
	}
	attrs := []string{}
	add := func(ok bool, a string) {
		if ok {
			attrs = append(attrs, a)
		}
	}
	add(c.IsRookie, "rookie")
	add(c.IsAutograph, "auto")
	add(c.IsMemorabilia, "patch")
	add(c.IsShortPrint, "short_print")
	add(c.IsErrorVariant, "error")
	add(s(c.SerialNumber) != "", "numbered")
	add(s(c.Variety) != "", "parallel")
	if edition == "tcg" {
		add(c.IsFoil, "foil")
		add(c.IsChase, "secret_rare")
	}
	price := 0
	if p := priceCents(c); p != nil {
		price = *p
	}
	var desc []string
	if v := s(c.Variety); v != "" {
		desc = append(desc, "Parallel: "+v+".")
	}
	if v := s(c.SerialNumber); v != "" {
		desc = append(desc, "Serial number: "+v+".")
	}
	if v := s(c.Team); v != "" {
		desc = append(desc, "Team: "+v+".")
	}
	if grader == "" && strings.TrimSpace(c.Condition) != "" {
		desc = append(desc, "Condition: "+strings.TrimSpace(c.Condition)+".")
	}
	cat, game, _ := placement(c, edition)
	in := Input{
		ExternalID: c.Sku, Category: cat, GameOrSport: game, SetName: strings.TrimSpace(c.SetName),
		Title: Title(c), Description: strings.Join(desc, " "), Attributes: attrs, PhotoURLs: photos,
		PriceCents: price, Quantity: 1, Format: "fixed", WeightOz: 1, PackagePreset: "bubble",
	}
	if c.Year > 0 {
		in.Year = ptr(c.Year)
	}
	if n := strings.TrimLeft(strings.TrimSpace(c.CardNumber), "#"); n != "" {
		in.CardNumber = &n
	}
	if grader == "" {
		in.Condition = ptr(MapCondition(c.Condition))
	} else {
		in.Grader, in.Grade = &grader, grade
		if edition == "tcg" && s(c.CertNumber) != "" {
			in.CertNumber = ptr(s(c.CertNumber))
		}
		in.WeightOz = 3
	}
	envelope := func(p int) bool { return grader == "" && p > 0 && p <= 2000 }
	if envelope(price) {
		in.PackagePreset = "envelope"
	}
	if o.Auction {
		start := 0
		if v := startCents(c, o); v != nil {
			start = *v
		}
		var reserve, bin *int
		if r := cents(c.ReservePrice); r != nil && *r > start {
			reserve = r
		}
		floor := start
		if reserve != nil {
			floor = *reserve
		}
		if b := cents(c.SalePrice); o.BuyItNowAtSalePrice && b != nil && *b > floor {
			bin = b
		}
		days := o.DurationDays
		if days <= 0 {
			days = 7
		}
		in.Format, in.PriceCents = "auction", start
		in.Auction = &Auction{StartCents: start, ReserveCents: reserve, BuyNowCents: bin, DurationDays: days}
		in.PackagePreset = "bubble"
		if bin != nil && envelope(*bin) {
			in.PackagePreset = "envelope"
		}
	} else if o.AllowOffers {
		in.Offers = &Offers{Enabled: true, AcceptKind: ptr("percent"), AcceptValue: o.AcceptPercent, DeclineKind: ptr("percent"), DeclineValue: o.DeclinePercent}
		if o.AcceptPercent == nil {
			in.Offers.AcceptKind = nil
		}
		if o.DeclinePercent == nil {
			in.Offers.DeclineKind = nil
		}
	} else {
		in.Offers = &Offers{Enabled: false}
	}
	// The seller's package defaults from FireCardManager.
	if grader == "" && ship.RawWeightOz > 0 {
		in.WeightOz = ship.RawWeightOz
	} else if grader != "" && ship.GradedWeightOz > 0 {
		in.WeightOz = ship.GradedWeightOz
	}
	if ship.PackageMode == "bubble" || ship.PackageMode == "box" {
		in.PackagePreset = ship.PackageMode
	}
	return in
}
