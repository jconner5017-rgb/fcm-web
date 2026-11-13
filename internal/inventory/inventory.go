// Package inventory answers the website's read-only questions about a
// seller's synced inventory: which boxes and rows they have, which cards
// match a search, one card in full, and the Sold Log.
//
// Everything reads the sync tables the desktop apps fill (see cloudsync).
// Card fields live in a JSON object named after the desktop's properties in
// snake_case (player_name, set_name, estimated_value, slot_row, ...).
package inventory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
)

const (
	soldStatus = "Sold"
	MaxPerPage = 120
)

var (
	ErrNotFound = errors.New("not found")
	uuidRE      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	rowRE       = regexp.MustCompile(`^[A-Za-z0-9 ._-]{1,20}$`)
	// Statuses a filter may ask for (the desktop's CardStatus names).
	statuses = map[string]bool{"NotForSale": true, "Ready": true, "EBA": true, "EBF": true}
)

type Service struct {
	DB     *sql.DB
	Sync   *cloudsync.Service // for edits from the website
	Photos PhotoSigner        // signed photo links for the browser (nil: none)
}

// PhotoSigner makes browser links for card photos (photos.Service).
type PhotoSigner interface {
	BrowserURLs(ctx context.Context, accountID string, ids []string) (map[string]string, error)
}

// photoIDs reads photo id fields out of a card's data.
func photoIDs(data []byte, keys ...string) map[string]string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(data, &m)
	out := map[string]string{}
	for _, k := range keys {
		var s string
		if json.Unmarshal(m[k], &s) == nil && s != "" {
			out[k] = strings.ToLower(s)
		}
	}
	return out
}

func (s *Service) sign(ctx context.Context, accountID string, ids []string) map[string]string {
	if s.Photos == nil || len(ids) == 0 {
		return map[string]string{}
	}
	urls, err := s.Photos.BrowserURLs(ctx, accountID, ids)
	if err != nil {
		return map[string]string{} // the page falls back to the slower photo links
	}
	return urls
}

// NewID makes a random (version 4) UUID for a row the website creates.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---------------- summary ----------------

type Edition struct {
	Edition string `json:"edition"`
	Cards   int    `json:"cards"`
}

type Row struct {
	Row   string `json:"row"`
	Cards int    `json:"cards"`
}

type Box struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	ColorTheme  string  `json:"color_theme,omitempty"`
	Cards       int     `json:"cards"`
	Value       float64 `json:"value"`
	Rows        []Row   `json:"rows"`
}

type Summary struct {
	Editions  []Edition      `json:"editions"`
	Edition   string         `json:"edition"`
	Boxes     []Box          `json:"boxes"`
	Unboxed   int            `json:"unboxed"`
	Cards     int            `json:"cards"`
	Value     float64        `json:"value"`
	ByStatus  map[string]int `json:"by_status"`
	Sold      int            `json:"sold"`
	Conflicts int            `json:"conflicts"`
	LastSync  *time.Time     `json:"last_sync,omitempty"`
}

// Editions lists the apps (sports, tcg) the account has synced cards or
// boxes from, with their inventory card counts.
func (s *Service) Editions(ctx context.Context, accountID string) ([]Edition, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT e.edition, (SELECT count(*) FROM sync_cards c WHERE c.account_id = $1 AND c.edition = e.edition
			AND c.deleted_at IS NULL AND coalesce(c.status, '') <> $2)
		FROM (SELECT edition FROM sync_cards WHERE account_id = $1 UNION
		      SELECT edition FROM sync_boxes WHERE account_id = $1 UNION
		      SELECT edition FROM sync_sold WHERE account_id = $1) e
		ORDER BY e.edition`, accountID, soldStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Edition{}
	for rows.Next() {
		var e Edition
		if err := rows.Scan(&e.Edition, &e.Cards); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PickEdition returns want when the account has it, else the edition with
// the most cards ("sports" when nothing has synced yet).
func PickEdition(eds []Edition, want string) string {
	best, bestN := "", -1
	for _, e := range eds {
		if e.Edition == want {
			return want
		}
		if e.Cards > bestN {
			best, bestN = e.Edition, e.Cards
		}
	}
	if best == "" {
		return "sports"
	}
	return best
}

// value is the card's estimated value as a number, or NULL.
const valueExpr = `CASE WHEN jsonb_typeof(c.data->'estimated_value') = 'number' THEN (c.data->>'estimated_value')::numeric END`

func (s *Service) Summary(ctx context.Context, accountID, edition string) (Summary, error) {
	eds, err := s.Editions(ctx, accountID)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Editions: eds, Edition: PickEdition(eds, edition), Boxes: []Box{}, ByStatus: map[string]int{}}
	ed := sum.Edition

	// Boxes with their card counts and value.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT b.id, coalesce(b.data->>'name', ''), coalesce(b.data->>'description', ''), coalesce(b.data->>'color_theme', ''),
			count(c.id), coalesce(sum(`+valueExpr+`), 0)
		FROM sync_boxes b
		LEFT JOIN sync_cards c ON c.box_id = b.id AND c.account_id = b.account_id AND c.deleted_at IS NULL AND coalesce(c.status, '') <> $3
		WHERE b.account_id = $1 AND b.edition = $2 AND b.deleted_at IS NULL
		GROUP BY b.id
		ORDER BY lower(coalesce(b.data->>'name', '')), b.id`, accountID, ed, soldStatus)
	if err != nil {
		return sum, err
	}
	idx := map[string]int{}
	for rows.Next() {
		var b Box
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.ColorTheme, &b.Cards, &b.Value); err != nil {
			rows.Close()
			return sum, err
		}
		b.Rows = []Row{}
		idx[b.ID] = len(sum.Boxes)
		sum.Boxes = append(sum.Boxes, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Rows in each box.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT c.box_id, coalesce(c.data->>'slot_row', ''), count(*)
		FROM sync_cards c
		WHERE c.account_id = $1 AND c.edition = $2 AND c.deleted_at IS NULL AND coalesce(c.status, '') <> $3 AND c.box_id IS NOT NULL
		GROUP BY 1, 2
		ORDER BY 1, length(coalesce(c.data->>'slot_row', '')), 2`, accountID, ed, soldStatus)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var box string
		var r Row
		if err := rows.Scan(&box, &r.Row, &r.Cards); err != nil {
			rows.Close()
			return sum, err
		}
		if i, ok := idx[box]; ok && r.Row != "" {
			sum.Boxes[i].Rows = append(sum.Boxes[i].Rows, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Totals.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT coalesce(c.status, ''), count(*), coalesce(sum(`+valueExpr+`), 0),
			count(*) FILTER (WHERE c.box_id IS NULL OR NOT EXISTS (SELECT 1 FROM sync_boxes b WHERE b.id = c.box_id AND b.deleted_at IS NULL))
		FROM sync_cards c
		WHERE c.account_id = $1 AND c.edition = $2 AND c.deleted_at IS NULL AND coalesce(c.status, '') <> $3
		GROUP BY 1`, accountID, ed, soldStatus)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var st string
		var n, unboxed int
		var v float64
		if err := rows.Scan(&st, &n, &v, &unboxed); err != nil {
			rows.Close()
			return sum, err
		}
		sum.ByStatus[st] += n
		sum.Cards += n
		sum.Value += v
		sum.Unboxed += unboxed
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	var last sql.NullTime
	err = s.DB.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM sync_sold WHERE account_id = $1 AND edition = $2 AND deleted_at IS NULL),
			(SELECT max(changed_at) FROM sync_changes WHERE account_id = $1),
			(SELECT count(*) FROM sync_conflicts WHERE account_id = $1 AND resolved_at IS NULL)`, accountID, ed).Scan(&sum.Sold, &last, &sum.Conflicts)
	if err != nil {
		return sum, err
	}
	if last.Valid {
		sum.LastSync = &last.Time
	}
	return sum, nil
}

// ---------------- card list ----------------

type Query struct {
	Edition string
	Box     string // box id, "none" for cards not in a box, "" for all
	Row     string // slot row within Box
	Status  string // NotForSale, Ready, EBA, EBF, "listed" (EBA or EBF), "" for all
	Search  string
	Sort    string // player, year, value, slot, updated, set
	Desc    bool
	Page    int // from 1
	PerPage int
}

type CardRow struct {
	ID        string          `json:"id"`
	BoxID     string          `json:"box_id,omitempty"`
	BoxName   string          `json:"box_name,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
	ThumbURL  string          `json:"thumb_url,omitempty"`
}

type CardPage struct {
	Cards   []CardRow `json:"cards"`
	Total   int       `json:"total"`
	Page    int       `json:"page"`
	PerPage int       `json:"per_page"`
}

var sorts = map[string]string{
	"player":  `lower(coalesce(c.data->>'player_name', ''))`,
	"set":     `lower(coalesce(c.data->>'set_name', ''))`,
	"year":    `c.data->'year'`,
	"value":   valueExpr,
	"updated": `c.updated_at`,
	"slot":    `lower(coalesce(b.data->>'name', '~')) %[1]s, length(coalesce(c.data->>'slot_row', '')) %[1]s, c.data->>'slot_row' %[1]s, c.data->'slot_number'`,
}

// searchText is what a search word is matched against.
const searchText = `concat_ws(' ', c.data->>'player_name', c.data->>'team', c.data->>'brand', c.data->>'set_name',
	c.data->>'card_number', c.data->>'variety', c.data->>'sku', c.data->>'year', c.data->>'serial_number',
	c.data->>'grading_company', c.data->>'card_grade', c.data->>'sport', c.data->>'condition', b.data->>'name')`

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// Words splits a search into at most 8 words.
func Words(q string) []string {
	f := strings.Fields(q)
	if len(f) > 8 {
		f = f[:8]
	}
	return f
}

func (s *Service) Cards(ctx context.Context, accountID string, q Query) (CardPage, error) {
	if q.PerPage <= 0 || q.PerPage > MaxPerPage {
		q.PerPage = 60
	}
	if q.Page < 1 {
		q.Page = 1
	}
	where := []string{"c.account_id = $1", "c.edition = $2", "c.deleted_at IS NULL", "coalesce(c.status, '') <> $3"}
	args := []any{accountID, q.Edition, soldStatus}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	switch {
	case q.Box == "none":
		where = append(where, "(c.box_id IS NULL OR b.id IS NULL)")
	case uuidRE.MatchString(q.Box):
		add("c.box_id = $%d", strings.ToLower(q.Box))
		if q.Row != "" && rowRE.MatchString(q.Row) {
			add("coalesce(c.data->>'slot_row', '') = $%d", q.Row)
		}
	}
	switch {
	case q.Status == "listed":
		where = append(where, "c.status IN ('EBA', 'EBF')")
	case statuses[q.Status]:
		add("c.status = $%d", q.Status)
	}
	for _, w := range Words(q.Search) {
		add(searchText+` ILIKE $%d`, likeEscape(w))
	}

	sortKey, ok := sorts[q.Sort]
	if !ok {
		sortKey = sorts["slot"]
		if q.Search != "" {
			sortKey = sorts["player"]
		}
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	var order string
	if strings.Contains(sortKey, "%[1]s") {
		order = fmt.Sprintf(sortKey, dir) + " " + dir
	} else {
		order = sortKey + " " + dir + " NULLS LAST"
	}

	args = append(args, q.PerPage, (q.Page-1)*q.PerPage)
	query := `
		SELECT c.id, coalesce(b.id::text, ''), coalesce(b.data->>'name', ''), c.updated_at, c.data, count(*) OVER ()
		FROM sync_cards c
		LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id AND b.deleted_at IS NULL
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + `, c.id
		LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return CardPage{}, err
	}
	defer rows.Close()
	out := CardPage{Cards: []CardRow{}, Page: q.Page, PerPage: q.PerPage}
	for rows.Next() {
		var r CardRow
		var data []byte
		if err := rows.Scan(&r.ID, &r.BoxID, &r.BoxName, &r.UpdatedAt, &data, &out.Total); err != nil {
			return out, err
		}
		r.Data = data
		out.Cards = append(out.Cards, r)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// One signed link per tile, straight to photo storage.
	want := make([]string, len(out.Cards))
	var ids []string
	for i, c := range out.Cards {
		p := photoIDs(c.Data, "front_thumb_photo_id", "front_photo_id")
		if id := p["front_thumb_photo_id"]; id != "" {
			want[i] = id
		} else {
			want[i] = p["front_photo_id"]
		}
		if want[i] != "" {
			ids = append(ids, want[i])
		}
	}
	urls := s.sign(ctx, accountID, ids)
	for i := range out.Cards {
		out.Cards[i].ThumbURL = urls[want[i]]
	}
	if len(out.Cards) == 0 && q.Page > 1 {
		// Past the end: report the total so the page can offer page 1.
		err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM sync_cards c
			LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id AND b.deleted_at IS NULL
			WHERE `+strings.Join(where, " AND "), args[:len(args)-2]...).Scan(&out.Total)
	}
	return out, err
}

// ---------------- one card ----------------

type Card struct {
	ID        string          `json:"id"`
	Edition   string          `json:"edition"`
	BoxID     string          `json:"box_id,omitempty"`
	BoxName   string          `json:"box_name,omitempty"`
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
	FrontURL  string          `json:"front_url,omitempty"`
	BackURL   string          `json:"back_url,omitempty"`
}

// CardWithPhotos is Card plus signed links for its front and back photos.
func (s *Service) CardWithPhotos(ctx context.Context, accountID, id string) (Card, error) {
	c, err := s.Card(ctx, accountID, id)
	if err != nil {
		return c, err
	}
	p := photoIDs(c.Data, "front_photo_id", "back_photo_id")
	var ids []string
	for _, v := range p {
		ids = append(ids, v)
	}
	urls := s.sign(ctx, accountID, ids)
	c.FrontURL, c.BackURL = urls[p["front_photo_id"]], urls[p["back_photo_id"]]
	return c, nil
}

func (s *Service) Card(ctx context.Context, accountID, id string) (Card, error) {
	if !uuidRE.MatchString(id) {
		return Card{}, ErrNotFound
	}
	var c Card
	var data []byte
	err := s.DB.QueryRowContext(ctx, `
		SELECT c.id, c.edition, coalesce(b.id::text, ''), coalesce(b.data->>'name', ''), c.version, c.updated_at, c.data
		FROM sync_cards c
		LEFT JOIN sync_boxes b ON b.id = c.box_id AND b.account_id = c.account_id AND b.deleted_at IS NULL
		WHERE c.id = $1 AND c.account_id = $2 AND c.deleted_at IS NULL`, strings.ToLower(id), accountID).
		Scan(&c.ID, &c.Edition, &c.BoxID, &c.BoxName, &c.Version, &c.UpdatedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	c.Data = data
	return c, err
}

// ---------------- Sold Log ----------------

type SoldRow struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type SoldPage struct {
	Sold    []SoldRow `json:"sold"`
	Total   int       `json:"total"`
	Revenue float64   `json:"revenue"` // sale prices of every match, not just this page
	Page    int       `json:"page"`
	PerPage int       `json:"per_page"`
}

const soldSearchText = `concat_ws(' ', c.data->>'player_name', c.data->>'team', c.data->>'brand', c.data->>'set_name',
	c.data->>'card_number', c.data->>'variety', c.data->>'year', c.data->>'buyer', c.data->>'notes', c.data->>'sport',
	c.data->>'tracking_number')`

func (s *Service) Sold(ctx context.Context, accountID, edition, search string, page, perPage int) (SoldPage, error) {
	if perPage <= 0 || perPage > MaxPerPage {
		perPage = 60
	}
	if page < 1 {
		page = 1
	}
	where := []string{"c.account_id = $1", "c.edition = $2", "c.deleted_at IS NULL"}
	args := []any{accountID, edition}
	for _, w := range Words(search) {
		args = append(args, likeEscape(w))
		where = append(where, fmt.Sprintf(soldSearchText+` ILIKE $%d`, len(args)))
	}
	out := SoldPage{Sold: []SoldRow{}, Page: page, PerPage: perPage}
	err := s.DB.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(CASE WHEN jsonb_typeof(c.data->'sale_price') = 'number'
			THEN (c.data->>'sale_price')::numeric END), 0)
		FROM sync_sold c WHERE `+strings.Join(where, " AND "), args...).Scan(&out.Total, &out.Revenue)
	if err != nil {
		return out, err
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.data FROM sync_sold c WHERE `+strings.Join(where, " AND ")+`
		ORDER BY c.data->>'sale_date' DESC NULLS LAST, c.id
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r SoldRow
		var data []byte
		if err := rows.Scan(&r.ID, &data); err != nil {
			return out, err
		}
		r.Data = data
		out.Sold = append(out.Sold, r)
	}
	return out, rows.Err()
}
