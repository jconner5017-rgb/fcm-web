// Package export writes the same CSV files as FireCardManager's desktop
// exports (Services/Export): the card database CSV (CardCsvRow's columns,
// so the desktop's own import reads it), Shopify's product import file and
// Whatnot's bulk upload file. Photos are the account's hosted links
// (/api/v1/p/<photo id>.jpg), so nothing has to be uploaded anywhere.
package export

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
)

// Card is a synced card's fields an export needs.
type Card struct {
	slabs.Card
	ID             string   `json:"-"`
	BoxName        string   `json:"-"`
	EstimatedValue *float64 `json:"estimated_value"`
	PaidPrice      *float64 `json:"paid_price"`
	LastPricedAt   *string  `json:"last_priced_at"`
	SlotRow        *string  `json:"slot_row"`
	SlotNumber     *int     `json:"slot_number"`
	FrontPhotoID   string   `json:"front_photo_id"`
	BackPhotoID    string   `json:"back_photo_id"`
	IsSketchCard   bool     `json:"is_sketch_card"`
}

// Parse reads a card's synced data.
func Parse(id, boxName string, data []byte) Card {
	var c Card
	_ = json.Unmarshal(data, &c)
	c.ID, c.BoxName = id, boxName
	return c
}

// PhotoLink is the hosted link for a photo id ("" for none).
type PhotoLink func(photoID string) string

// HostedLinks makes the permanent links the desktop uses too:
// <web url>/api/v1/p/<photo id>.jpg.
func HostedLinks(webURL string) PhotoLink {
	base := strings.TrimRight(webURL, "/")
	return func(id string) string {
		if id == "" {
			return ""
		}
		return base + "/api/v1/p/" + strings.ToLower(id) + ".jpg"
	}
}

// Writer builds a CSV the way the desktop does: CRLF lines, quoted only
// when needed, UTF-8 without a BOM.
type Writer struct{ b strings.Builder }

func (w *Writer) Row(cells ...string) {
	for i, v := range cells {
		if i > 0 {
			w.b.WriteByte(',')
		}
		if strings.ContainsAny(v, ",\"\n\r") {
			v = `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
		}
		w.b.WriteString(v)
	}
	w.b.WriteString("\r\n")
}

func (w *Writer) String() string { return w.b.String() }

func money(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// ---------------- the card database CSV ----------------

// CardColumns are CardCsvRow's columns, in its order, so FireCardManager's
// "Import Cards from CSV" reads the file. The extra columns after them carry
// what the web keeps too (the desktop import ignores columns it doesn't know).
var CardColumns = []string{"Id", "Sport", "PlayerName", "Team", "Year", "Brand", "SetName", "CardNumber", "Variety", "Condition",
	"EstimatedValue", "FrontImageFileName", "BackImageFileName", "BoxName", "SlotRow", "SlotNumber", "LastPricedAt",
	"Sku", "SeasonEnd", "SerialNumber", "GradingCompany", "CardGrade", "CertNumber", "SalePrice", "StartBid", "PaidPrice", "Status",
	"IsRookie", "IsAutograph", "IsMemorabilia", "IsShortPrint", "IsErrorVariant", "IsChase", "IsFoil", "IsSketchCard"}

func yes(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func optMoney(p *float64) string {
	if p == nil {
		return ""
	}
	return money(*p)
}

// lastPriced in the format the desktop import accepts (yyyy-MM-ddTHH:mm:ss).
func lastPriced(p *string) string {
	if p == nil || *p == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, *p)
	if err != nil {
		if t, err = time.Parse("2006-01-02T15:04:05", *p); err != nil {
			return ""
		}
	}
	return t.UTC().Format("2006-01-02T15:04:05")
}

// CardsCSV is the database export. The image columns hold the hosted photo
// links instead of file names.
func CardsCSV(cards []Card, link PhotoLink) string {
	var w Writer
	w.Row(CardColumns...)
	for _, c := range cards {
		year, season, slot := "", "", ""
		if c.Year > 0 {
			year = strconv.Itoa(c.Year)
		}
		if c.SeasonEnd != nil {
			season = strconv.Itoa(*c.SeasonEnd)
		}
		if c.SlotNumber != nil {
			slot = strconv.Itoa(*c.SlotNumber)
		}
		est := "0.00"
		if c.EstimatedValue != nil {
			est = money(*c.EstimatedValue)
		}
		w.Row("", c.Sport, c.PlayerName, str(c.Team), year, c.Brand, c.SetName, c.CardNumber, str(c.Variety), c.Condition,
			est, link(c.FrontPhotoID), link(c.BackPhotoID), c.BoxName, str(c.SlotRow), slot, lastPriced(c.LastPricedAt),
			c.Sku, season, str(c.SerialNumber), str(c.GradingCompany), str(c.CardGrade), str(c.CertNumber),
			optMoney(c.SalePrice), optMoney(c.StartBid), optMoney(c.PaidPrice), c.Status,
			yes(c.IsRookie), yes(c.IsAutograph), yes(c.IsMemorabilia), yes(c.IsShortPrint), yes(c.IsErrorVariant),
			yes(c.IsChase), yes(c.IsFoil), yes(c.IsSketchCard))
	}
	return w.String()
}

// ---------------- Shopify and Whatnot (MarketplaceCsvExporter) ----------------

type ShopifyOptions struct {
	Vendor      string `json:"vendor"`
	ProductType string `json:"product_type"`
	Tags        string `json:"tags"`
	Active      bool   `json:"active"`
}

type WhatnotOptions struct {
	Category        string `json:"category"`
	SubCategory     string `json:"sub_category"`
	Auction         bool   `json:"auction"`
	ShippingProfile string `json:"shipping_profile"`
	Offerable       bool   `json:"offerable"`
}

// Result is the file plus a note per card that was skipped or has no photo.
type Result struct {
	CSV      string   `json:"-"`
	Exported int      `json:"exported"`
	Notes    []string `json:"notes"`
}

// Price is the asking price: the sale price, else the estimate.
func Price(c Card) *float64 {
	if c.SalePrice != nil && *c.SalePrice > 0 {
		return c.SalePrice
	}
	if c.EstimatedValue != nil && *c.EstimatedValue > 0 {
		return c.EstimatedValue
	}
	return nil
}

func graded(c Card) bool {
	g := str(c.GradingCompany)
	return g != "" && !strings.EqualFold(g, "raw")
}

func gradeText(c Card) string { return strings.TrimSpace(str(c.GradingCompany) + " " + str(c.CardGrade)) }

// ConditionName turns "Raw - NM" into "Near Mint".
func ConditionName(cond string) string {
	s := strings.ToUpper(cond)
	switch {
	case strings.Contains(s, "NM") || strings.Contains(s, "NEAR MINT") || strings.Contains(s, "MINT"):
		return "Near Mint"
	case strings.Contains(s, "LP") || strings.Contains(s, "LIGHT"):
		return "Lightly Played"
	case strings.Contains(s, "MP") || strings.Contains(s, "MODERATE"):
		return "Moderately Played"
	case strings.Contains(s, "HP") || strings.Contains(s, "HEAVY"):
		return "Heavily Played"
	case strings.Contains(s, "DMG") || strings.Contains(s, "DAMAGE") || strings.Contains(s, "POOR"):
		return "Damaged"
	}
	return "Near Mint"
}

func yearLabel(c Card) string {
	if c.SeasonEnd != nil && *c.SeasonEnd != c.Year {
		return fmt.Sprintf("%d-%02d", c.Year, *c.SeasonEnd%100)
	}
	return strconv.Itoa(c.Year)
}

func details(c Card) []string {
	var d []string
	var parts []string
	if c.Year > 0 {
		parts = append(parts, yearLabel(c))
	}
	for _, v := range []string{c.Brand, c.SetName} {
		if strings.TrimSpace(v) != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) > 0 {
		d = append(d, strings.Join(parts, " "))
	}
	if strings.TrimSpace(c.PlayerName) != "" {
		line := c.PlayerName
		if n := strings.TrimLeft(strings.TrimSpace(c.CardNumber), "#"); n != "" {
			line += " #" + n
		}
		d = append(d, line)
	}
	if t := str(c.Team); t != "" {
		d = append(d, "Team: "+t)
	}
	if v := str(c.Variety); v != "" {
		d = append(d, "Parallel: "+v)
	}
	if v := str(c.SerialNumber); v != "" {
		d = append(d, "Serial: "+v)
	}
	var flags []string
	if c.IsRookie {
		flags = append(flags, "Rookie card")
	}
	if c.IsAutograph {
		flags = append(flags, "Autograph")
	}
	if c.IsMemorabilia {
		flags = append(flags, "Memorabilia/relic")
	}
	if len(flags) > 0 {
		d = append(d, strings.Join(flags, ", "))
	}
	if graded(c) {
		d = append(d, "Graded: "+gradeText(c))
	} else {
		d = append(d, "Condition: "+ConditionName(c.Condition))
	}
	return append(d, "SKU: "+c.Sku)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func handle(title, sku string) string {
	h := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title+" "+sku), "-"), "-")
	if len(h) > 200 {
		h = strings.Trim(h[:200], "-")
	}
	return h
}

func trim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimRight(string(r[:n]), " ")
	}
	return s
}

func links(c Card, link PhotoLink, notes *[]string) []string {
	var out []string
	for _, id := range []string{c.FrontPhotoID, c.BackPhotoID} {
		if u := link(id); u != "" {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		*notes = append(*notes, fmt.Sprintf("%s %s: no front/back photo to include", c.Sku, c.PlayerName))
	}
	return out
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// Shopify writes Shopify's product import file: one product per card,
// quantity 1, extra rows for the extra photos.
func Shopify(cards []Card, o ShopifyOptions, link PhotoLink) Result {
	var w Writer
	var notes []string
	w.Row("Handle", "Title", "Body (HTML)", "Vendor", "Type", "Tags", "Published", "Option1 Name", "Option1 Value",
		"Variant SKU", "Variant Grams", "Variant Inventory Tracker", "Variant Inventory Qty", "Variant Inventory Policy",
		"Variant Fulfillment Service", "Variant Price", "Variant Requires Shipping", "Variant Taxable",
		"Image Src", "Image Position", "Image Alt Text", "Status")
	n := 0
	for _, c := range cards {
		price := Price(c)
		if price == nil {
			notes = append(notes, fmt.Sprintf("%s %s: skipped - no price", c.Sku, c.PlayerName))
			continue
		}
		urls := links(c, link, &notes)
		title := slabs.Title(c.Card)
		h := handle(title, c.Sku)
		var tags []string
		for _, t := range []string{o.Tags, c.Sport, map[bool]string{true: "Rookie"}[c.IsRookie], map[bool]string{true: "Autograph"}[c.IsAutograph],
			map[bool]string{true: "Graded"}[graded(c)]} {
			if strings.TrimSpace(t) != "" {
				tags = append(tags, t)
			}
		}
		var body []string
		for _, d := range details(c) {
			body = append(body, html.EscapeString(d))
		}
		grams, pub, status := "30", "FALSE", "draft"
		if graded(c) {
			grams = "150"
		}
		if o.Active {
			pub, status = "TRUE", "active"
		}
		pos, alt := "", ""
		if len(urls) > 0 {
			pos, alt = "1", title
		}
		w.Row(h, title, "<p>"+strings.Join(body, "<br>")+"</p>", o.Vendor, o.ProductType, strings.Join(tags, ", "), pub, "Title", "Default Title",
			c.Sku, grams, "shopify", "1", "deny", "manual", money(*price), "TRUE", "TRUE", at(urls, 0), pos, alt, status)
		for i := 1; i < len(urls); i++ {
			w.Row(h, "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", urls[i], strconv.Itoa(i+1), title, "")
		}
		n++
	}
	return Result{CSV: w.String(), Exported: n, Notes: notes}
}

// DefaultSubCategory is Whatnot's sub category guessed from the card's category.
func DefaultSubCategory(sport string) string {
	switch sport {
	case "Mma":
		return "MMA Cards"
	case "Racing":
		return "Racing Cards"
	case "Multisport":
		return "Other Sports Cards"
	case "Pokemon":
		return "Pokémon Cards"
	case "Magic":
		return "Magic: The Gathering Cards"
	case "YuGiOh":
		return "Yu-Gi-Oh! Cards"
	case "OnePiece":
		return "One Piece Cards"
	}
	return sport + " Cards"
}

// Whatnot writes Whatnot's Seller Hub bulk upload file.
func Whatnot(cards []Card, o WhatnotOptions, link PhotoLink) Result {
	var w Writer
	var notes []string
	header := []string{"Category", "Sub Category", "Title", "Description", "Quantity", "Type", "Price",
		"Shipping Profile", "Offerable", "Hazmat", "Condition", "Cost Per Item", "SKU"}
	for i := 1; i <= 8; i++ {
		header = append(header, fmt.Sprintf("Image URL %d", i))
	}
	w.Row(header...)
	n := 0
	for _, c := range cards {
		price := Price(c)
		if o.Auction && c.StartBid != nil && *c.StartBid > 0 {
			price = c.StartBid
		}
		if price == nil {
			notes = append(notes, fmt.Sprintf("%s %s: skipped - no price", c.Sku, c.PlayerName))
			continue
		}
		urls := links(c, link, &notes)
		sub := o.SubCategory
		if strings.TrimSpace(sub) == "" {
			sub = DefaultSubCategory(c.Sport)
		}
		kind, offer, cond, cost := "Buy it Now", "FALSE", ConditionName(c.Condition), ""
		if o.Auction {
			kind = "Auction"
		} else if o.Offerable {
			offer = "TRUE"
		}
		if graded(c) {
			cond = "Graded"
		}
		if c.PaidPrice != nil && *c.PaidPrice > 0 {
			cost = money(*c.PaidPrice)
		}
		cells := []string{o.Category, sub, trim(slabs.Title(c.Card), 80), strings.Join(details(c), "\n"), "1", kind, money(*price),
			o.ShippingProfile, offer, "Not Hazmat", cond, cost, c.Sku}
		for i := 0; i < 8; i++ {
			cells = append(cells, at(urls, i))
		}
		w.Row(cells...)
		n++
	}
	return Result{CSV: w.String(), Exported: n, Notes: notes}
}
