// Package identify names a card from photos of its front and back with the
// seller's own AI key (Claude or Gemini), using the same instructions as the
// FireCardManager desktop apps, and turns the answer into card fields.
package identify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultClaudeModel = "claude-haiku-5-5"
	DefaultGeminiModel = "gemini-flash-lite-latest"
	DefaultSalePercent = 25 // the desktop's default markup over the AI estimate
	toolName           = "submit_card_identification"
)

var ErrBadKey = errors.New("the AI service didn't accept the key")

// Result is the model's answer (the desktop's CardIdentification).
type Result struct {
	Category          *string  `json:"category"`
	PlayerName        *string  `json:"playerName"`
	Team              *string  `json:"team"`
	Year              *int     `json:"year"`
	Season            *string  `json:"season"`
	Brand             *string  `json:"brand"`
	SetName           *string  `json:"setName"`
	CardNumber        *string  `json:"cardNumber"`
	Variety           *string  `json:"variety"`
	SerialNumber      *string  `json:"serialNumber"`
	Condition         *string  `json:"condition"`
	IsRookie          *bool    `json:"isRookie"`
	IsAutograph       *bool    `json:"isAutograph"`
	IsMemorabilia     *bool    `json:"isMemorabilia"`
	IsChase           *bool    `json:"isChase"`
	IsFoil            *bool    `json:"isFoil"`
	IsSketchCard      *bool    `json:"isSketchCard"`
	EstimatedValueUSD *float64 `json:"estimatedValueUsd"`
	Confidence        *float64 `json:"confidence"`
	Notes             *string  `json:"notes"`
}

// Client calls the AI services. BaseURLs can be pointed at a test server.
type Client struct {
	HTTP       *http.Client
	ClaudeURL  string // https://api.anthropic.com/v1/messages
	GeminiBase string // https://generativelanguage.googleapis.com/v1beta/models
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 90 * time.Second}
}

// Identify sends both photos (JPEG) to the provider and returns its answer.
func (c *Client) Identify(ctx context.Context, provider, key, model, edition string, front, back []byte) (Result, error) {
	switch provider {
	case "claude":
		return c.claude(ctx, key, model, edition, front, back)
	case "gemini":
		return c.gemini(ctx, key, model, edition, front, back)
	}
	return Result{}, fmt.Errorf("unknown AI provider %q", provider)
}

// ---------------- prompts (copied from the desktop apps) ----------------

func systemPrompt(edition string) string {
	if edition == "tcg" {
		return "You are an expert non-sport trading card grader and price appraiser. You identify non-sport " +
			"trading cards - movie, TV, comic/superhero, sci-fi, fantasy, anime, horror, wrestling, music " +
			"and other pop-culture franchises (Star Wars, Marvel, DC, Garbage Pail Kids, The Walking Dead, " +
			"and similar) - from photos of their front and back with precision. You pay close attention " +
			"to small print: copyright lines, card numbers, parallel/insert markings (e.g. foil stamps, " +
			"serial numbering like '/99', refractor patterns, chase-insert markings, sketch-card " +
			"indicators), and grading slab labels (PSA, BGS, SGC, CGC) including their numeric grade. If " +
			"a detail genuinely cannot be determined from the images, use null for that field rather than " +
			"guessing - and say why in the notes field. You also give a realistic current market value " +
			"estimate in US dollars, based on what comparable cards (same character/subject, franchise, " +
			"year, brand, set, parallel and condition) have actually been selling for recently (e.g. eBay " +
			"sold listings) to the best of your knowledge - a reasonable single-number midpoint estimate, " +
			"not a round guess, and null if you have no real basis for one (e.g. an obscure/unidentifiable " +
			"card). Submit your answer using the submit_card_identification tool, matching its schema exactly."
	}
	return "You are an expert sports card grader and price appraiser. You identify sports trading " +
		"cards (baseball, basketball, football, and others) from photos of their front and back " +
		"with precision. You pay close attention to small print: copyright lines, card numbers, " +
		"parallel/insert markings (e.g. foil stamps, serial numbering like '/99', refractor patterns, " +
		"'Cracked Ice', 'Silver', 'Gold', 'Prizm', holographic patterns), and grading slab labels " +
		"(PSA, BGS, SGC, CSG) including their numeric grade. If a detail genuinely cannot be " +
		"determined from the images, use null for that field rather than guessing - and say why in " +
		"the notes field. You also give a realistic current market value estimate in US dollars, " +
		"based on what comparable cards (same player, year, brand, set, parallel and condition) have " +
		"actually been selling for recently (e.g. eBay sold listings) to the best of your knowledge - " +
		"a reasonable single-number midpoint estimate, not a round guess, and null if you have no " +
		"real basis for one (e.g. an obscure/unidentifiable card). Submit your answer using the " +
		"submit_card_identification tool, matching its schema exactly."
}

func userPrompt(edition string) string {
	head := "Identify this trading card from its front and back images (attached), and estimate its current market value.\n\n" +
		"These are phone photos; the card may be at a slight angle or have glare - read what you can.\n\n"
	if edition == "tcg" {
		return head + "Call submit_card_identification with: category (the card's broad genre - exactly " +
			`one of "Pokemon", "Magic", "YuGiOh", "OnePiece", "Lorcana", "DragonBall", "Digimon", "FleshAndBlood", "FinalFantasy", "StarWarsTcg", "WeissSchwarz", "Vanguard", "Naruto", "MetaZoo", "Riftbound", "WorldOfWarcraft", "MyLittlePony", "Sorcery", "Netrunner", "UniVersus", "OtherTcg" for a card from a trading card game, or for any other non-sport card one of "Superheroes", "SciFi", "Fantasy", "Anime", "Horror", "Entertainment", "Music", "Historical", "Novelty" - pick the closest match), playerName (the character or real subject depicted, e.g. ` +
			`"Darth Vader"), team (the franchise/show/movie/series, e.g. "Star Wars"), ` +
			`year, brand (manufacturer, e.g. "Topps", "Upper Deck", "Panini"), setName (the set name only, with NO year and NO game or category name, e.g. "Scarlet & Violet" not "2023 Pokémon Scarlet & Violet"), ` +
			`cardNumber, variety (the parallel/insert name if any, e.g. "Refractor", "Holofoil", "Prism", or null for a plain base card), serialNumber (the print ` +
			`run number stamped/printed on a numbered parallel, read exactly as shown on the card - e.g. "23/99", "023/099" - or null if the card isn't serial-numbered), ` +
			`condition - use this exact abbreviated wording, not a spelled-out version (e.g. "Raw - NM", "Raw - Excellent", "Raw - Very Good", "Raw - Poor", "Graded - PSA 10"), ` +
			`isRookie (true if this is a promotional/exclusive card - a convention exclusive, promo insert, or similar marking that it isn't a standard base/retail ` +
			`card), isAutograph (true if the card bears a genuine on-card autograph, not a printed facsimile signature), isMemorabilia (true if the card contains an ` +
			`embedded piece of memorabilia - fabric swatch, film cel, etc.), isChase (true if this is the hardest-to-pull "chase" insert/parallel in its set), isFoil (true if ` +
			`the card is foil-stamped/foil-board), isSketchCard (true if this is a hand-drawn sketch/art card - one-of-one original art, not a printed parallel), ` +
			`estimatedValueUsd (your best current market-value estimate in US dollars for THIS card in THIS condition, as a plain number like 12.50, or null if you can't form a ` +
			`reasonable estimate), a confidence score from 0 to 1, and notes on anything uncertain (including, briefly, what the value estimate is based on).`
	}
	return head + `Call submit_card_identification with: category (the card's sport - exactly one of "Baseball", "Basketball", "Boxing", "Football", "Golf", ` +
		`"Hockey", "Mma", "Multisport", "Racing", "Soccer", "Tennis", "Wrestling" - pick the closest match), ` +
		`playerName, team, year (a number; for a split-season card like basketball or hockey, the first year, e.g. 2024 for 2024-25), season (the season of a split-season card as printed, e.g. "2024-25", otherwise null), brand, setName (the product or set name only, with NO year and NO sport or league name, e.g. "Prizm - Global Reach" or "Topps Chrome Update", not "2024-25 Panini Prizm Basketball - Global Reach"), cardNumber, variety (the parallel/insert ` +
		`name if any, e.g. "Refractor", "Cracked Ice", "Silver", or null for a plain base card), serialNumber (the print run number stamped/printed on a numbered ` +
		`parallel, read exactly as shown on the card - e.g. "23/99", "023/099" - or null if the card isn't serial-numbered), condition - use this exact abbreviated wording, not a spelled-out ` +
		`version (e.g. "Raw - NM", "Raw - EX", "Raw - VG", "Graded - PSA 10"), isRookie (true if this is a rookie card - a visible rookie logo/shield, "Rookie ` +
		`Card"/"RC" text, or the set's own rookie-year designation), isAutograph (true if the card bears a genuine on-card autograph, not a printed facsimile signature), ` +
		`isMemorabilia (true if the card contains an embedded piece of memorabilia - jersey, bat, patch, etc.), estimatedValueUsd (your best current market-value estimate in US ` +
		`dollars for THIS card in THIS condition, as a plain number like 12.50, or null if you can't form a reasonable estimate), a confidence score from 0 to 1, and notes ` +
		`on anything uncertain (including, briefly, what the value estimate is based on).`
}

func fieldsFor(edition string) []string {
	f := []string{"category", "playerName", "team", "year", "season", "brand", "setName", "cardNumber", "variety", "serialNumber", "condition",
		"isRookie", "isAutograph", "isMemorabilia"}
	if edition == "tcg" {
		f = append(f, "isChase", "isFoil", "isSketchCard")
	}
	return append(f, "estimatedValueUsd", "confidence", "notes")
}

func fieldType(name string) string {
	switch {
	case name == "year":
		return "integer"
	case name == "estimatedValueUsd" || name == "confidence":
		return "number"
	case strings.HasPrefix(name, "is"):
		return "boolean"
	}
	return "string"
}

// ---------------- Claude ----------------

func (c *Client) claude(ctx context.Context, key, model, edition string, front, back []byte) (Result, error) {
	if model == "" {
		model = DefaultClaudeModel
	}
	props := map[string]any{}
	for _, f := range fieldsFor(edition) {
		props[f] = map[string]any{"type": []string{fieldType(f), "null"}}
	}
	img := func(b []byte) map[string]any {
		return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": base64.StdEncoding.EncodeToString(b)}}
	}
	body := map[string]any{
		"model": model, "max_tokens": 1024, "system": systemPrompt(edition),
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": userPrompt(edition)}, img(front), img(back),
		}}},
		"tools": []any{map[string]any{"name": toolName, "description": "Submit the card identification and value estimate.",
			"input_schema": map[string]any{"type": "object", "properties": props, "required": fieldsFor(edition)}}},
		"tool_choice": map[string]any{"type": "tool", "name": toolName},
	}
	u := c.ClaudeURL
	if u == "" {
		u = "https://api.anthropic.com/v1/messages"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.do(req)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return Result{}, fmt.Errorf("Claude answer unreadable: %w", err)
	}
	for _, p := range out.Content {
		if p.Type == "tool_use" && p.Name == toolName {
			var r Result
			if err := json.Unmarshal(p.Input, &r); err != nil {
				return Result{}, fmt.Errorf("Claude answer unreadable: %w", err)
			}
			return r, nil
		}
	}
	return Result{}, errors.New("Claude didn't return an identification")
}

// ---------------- Gemini ----------------

func (c *Client) gemini(ctx context.Context, key, model, edition string, front, back []byte) (Result, error) {
	if model == "" {
		model = DefaultGeminiModel
	}
	props := map[string]any{}
	for _, f := range fieldsFor(edition) {
		props[f] = map[string]any{"type": strings.ToUpper(fieldType(f)), "nullable": true}
	}
	img := func(b []byte) map[string]any {
		return map[string]any{"inline_data": map[string]any{"mime_type": "image/jpeg", "data": base64.StdEncoding.EncodeToString(b)}}
	}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": systemPrompt(edition)}}},
		"contents": []any{map[string]any{"role": "user", "parts": []any{
			map[string]any{"text": strings.ReplaceAll(userPrompt(edition), "Call submit_card_identification with:", "Answer as JSON with:")}, img(front), img(back),
		}}},
		"generationConfig": map[string]any{"responseMimeType": "application/json",
			"responseSchema": map[string]any{"type": "OBJECT", "properties": props, "required": fieldsFor(edition)}},
	}
	base := c.GeminiBase
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta/models"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+url.PathEscape(model)+":generateContent", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", key) // header, so the key never appears in a URL or log
	resp, err := c.do(req)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return Result{}, errors.New("Gemini didn't return an identification")
	}
	var r Result
	if err := json.Unmarshal([]byte(out.Candidates[0].Content.Parts[0].Text), &r); err != nil {
		return Result{}, fmt.Errorf("Gemini answer unreadable: %w", err)
	}
	return r, nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the AI service: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, ErrBadKey
	case resp.StatusCode == 400 && bytes.Contains(b, []byte("API_KEY_INVALID")):
		return nil, ErrBadKey
	case resp.StatusCode >= 300:
		msg := string(b)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("AI service error %d: %s", resp.StatusCode, strings.TrimSpace(msg))
	}
	return b, nil
}

// ---------------- to card fields ----------------

var (
	yearPrefixRE = regexp.MustCompile(`^\s*\d{4}(\s*[-/]\s*\d{2,4})?\s+`)
	seasonRE     = regexp.MustCompile(`^(\d{4})\s*[-/]\s*(\d{2,4})$`)
)

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// cleanSet drops a leading year and the sport/category word from a set name
// (they live in their own fields), like the desktop's CardText.CleanCard.
func cleanSet(set, category string) string {
	s := yearPrefixRE.ReplaceAllString(set, "")
	if category != "" {
		re := regexp.MustCompile(`(?i)\s*\b` + regexp.QuoteMeta(category) + `\b`)
		s = re.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "-"))
}

// Fields turns the answer into a new card's fields (wire names), applying
// the desktop's defaults: Raw - NM condition, sale price 25% over the
// estimate, Ready once it has a name and a price.
func Fields(r Result, edition string, now time.Time) map[string]any {
	f := map[string]any{}
	cat := str(r.Category)
	if c := canonicalCategory(cat, edition); c != "" {
		f["sport"] = c
	}
	name := str(r.PlayerName)
	f["player_name"] = name
	if t := str(r.Team); t != "" {
		f["team"] = t
	}
	if r.Year != nil && *r.Year >= 1800 && *r.Year <= 2100 {
		f["year"] = *r.Year
	}
	if m := seasonRE.FindStringSubmatch(str(r.Season)); m != nil {
		end := m[2]
		if len(end) == 2 {
			end = m[1][:2] + end
		}
		var y, e int
		fmt.Sscan(m[1], &y)
		fmt.Sscan(end, &e)
		if e > y {
			f["year"], f["season_end"] = y, e
		}
	}
	f["brand"] = str(r.Brand)
	f["set_name"] = cleanSet(str(r.SetName), cat)
	f["card_number"] = strings.TrimPrefix(str(r.CardNumber), "#")
	if v := str(r.Variety); v != "" {
		f["variety"] = v
	}
	if v := str(r.SerialNumber); v != "" {
		f["serial_number"] = v
	}
	cond := str(r.Condition)
	if strings.EqualFold(strings.Join(strings.Fields(strings.ReplaceAll(cond, "-", " ")), " "), "Raw Near Mint") || cond == "" {
		cond = "Raw - NM"
	}
	f["condition"] = cond
	if strings.HasPrefix(strings.ToLower(cond), "graded") {
		// "Graded - PSA 10" -> company PSA, grade 10
		rest := strings.TrimSpace(strings.TrimLeft(cond[len("graded"):], " -"))
		if parts := strings.Fields(rest); len(parts) >= 2 {
			f["grading_company"], f["card_grade"] = parts[0], strings.Join(parts[1:], " ")
			f["condition"] = "Graded"
		}
	}
	for k, p := range map[string]*bool{"is_rookie": r.IsRookie, "is_autograph": r.IsAutograph, "is_memorabilia": r.IsMemorabilia,
		"is_chase": r.IsChase, "is_foil": r.IsFoil, "is_sketch_card": r.IsSketchCard} {
		if p != nil && (edition == "tcg" || (k != "is_chase" && k != "is_foil" && k != "is_sketch_card")) {
			f[k] = *p
		}
	}
	status := "NotForSale"
	if r.EstimatedValueUSD != nil && *r.EstimatedValueUSD > 0 && *r.EstimatedValueUSD < 1_000_000 {
		est := math.Round(*r.EstimatedValueUSD*100) / 100
		f["estimated_value"] = est
		f["sale_price_percent"] = DefaultSalePercent
		f["sale_price"] = math.Round(est*(1+DefaultSalePercent/100.0)*100) / 100
		f["last_priced_at"] = now.UTC().Format(time.RFC3339)
		if name != "" {
			status = "Ready"
		}
	}
	f["status"] = status
	return f
}

var sportsCats = []string{"Baseball", "Basketball", "Boxing", "Football", "Golf", "Hockey", "Mma", "Multisport", "Racing", "Soccer", "Tennis", "Wrestling", "Fighting"}
var tcgCats = []string{"Pokemon", "Magic", "YuGiOh", "OnePiece", "Lorcana", "DragonBall", "Digimon", "FleshAndBlood", "FinalFantasy", "StarWarsTcg",
	"WeissSchwarz", "Vanguard", "Naruto", "MetaZoo", "Riftbound", "WorldOfWarcraft", "MyLittlePony", "Sorcery", "Netrunner", "UniVersus", "OtherTcg",
	"Superheroes", "SciFi", "Fantasy", "Anime", "Horror", "Entertainment", "Music", "Historical", "Novelty"}

// canonicalCategory matches the desktop's enum names case-insensitively.
func canonicalCategory(c, edition string) string {
	list := sportsCats
	if edition == "tcg" {
		list = tcgCats
	}
	for _, x := range list {
		if strings.EqualFold(x, strings.ReplaceAll(c, " ", "")) {
			return x
		}
	}
	return "" // not one the app knows: the seller picks it
}
