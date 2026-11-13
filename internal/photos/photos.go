// Package photos stores card photos in a private S3-compatible bucket
// (Cloudflare R2). The API never handles photo bytes: a device asks for a
// signed upload link per photo, uploads straight to the bucket, then
// confirms. On confirm the card's <side>_photo_id field is set through the
// sync feed, so other devices and the website see the new photo.
package photos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/lib/pq"
)

const (
	MaxPhotoBytes = 10 << 20 // a 600 dpi scan is far smaller
	MaxPerCall    = 50
	UploadTTL     = 15 * time.Minute
	ViewTTL       = 10 * time.Minute
)

var (
	ErrNotConfigured = errors.New("photo storage isn't set up on this server yet")
	ErrNotFound      = errors.New("photo not found")
	ErrTooMany       = fmt.Errorf("ask for at most %d upload links at a time", MaxPerCall)
	uuidRE           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sides            = map[string]bool{"front": true, "back": true, "front_thumb": true, "back_thumb": true}
)

type Service struct {
	DB    *sql.DB
	Store *S3 // nil: photos are off
	Sync  *cloudsync.Service
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Enabled reports whether a bucket is configured.
func (s *Service) Enabled() bool { return s != nil && s.Store != nil }

// UploadRequest is one photo a device wants to upload.
type UploadRequest struct {
	CardID string `json:"card_id"`
	Side   string `json:"side"` // front, back, front_thumb, back_thumb
	Bytes  int64  `json:"bytes"`
}

// UploadSlot is where to PUT it (Content-Type: image/jpeg).
type UploadSlot struct {
	CardID    string    `json:"card_id"`
	Side      string    `json:"side"`
	PhotoID   string    `json:"photo_id,omitempty"`
	UploadURL string    `json:"upload_url,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// UploadURLs hands out one signed upload link per requested photo.
func (s *Service) UploadURLs(ctx context.Context, d cloudsync.Device, reqs []UploadRequest) ([]UploadSlot, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(reqs) > MaxPerCall {
		return nil, ErrTooMany
	}
	out := make([]UploadSlot, 0, len(reqs))
	for _, r := range reqs {
		slot := UploadSlot{CardID: strings.ToLower(r.CardID), Side: r.Side}
		switch {
		case !uuidRE.MatchString(slot.CardID):
			slot.Error = "card_id must be the card's sync id"
		case !sides[r.Side]:
			slot.Error = "side must be front, back, front_thumb or back_thumb"
		case r.Bytes <= 0 || r.Bytes > MaxPhotoBytes:
			slot.Error = "photo must be between 1 byte and 10 MB"
		}
		if slot.Error == "" {
			var ok bool
			if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM sync_cards
				WHERE id = $1 AND account_id = $2 AND edition = $3 AND deleted_at IS NULL)`,
				slot.CardID, d.AccountID, d.Edition).Scan(&ok); err != nil {
				return nil, err
			}
			if !ok {
				slot.Error = "card not found (sync the card before its photos)"
			}
		}
		if slot.Error != "" {
			out = append(out, slot)
			continue
		}
		var id string
		if err := s.DB.QueryRowContext(ctx, `INSERT INTO sync_photos (account_id, card_id, side, object_key, bytes)
			VALUES ($1, $2, $3, 'pending-' || gen_random_uuid(), $4) RETURNING id`,
			d.AccountID, slot.CardID, r.Side, r.Bytes).Scan(&id); err != nil {
			return nil, err
		}
		key := objectKey(d.AccountID, slot.CardID, r.Side, id)
		if _, err := s.DB.ExecContext(ctx, `UPDATE sync_photos SET object_key = $2 WHERE id = $1`, id, key); err != nil {
			return nil, err
		}
		slot.PhotoID = id
		slot.ExpiresAt = s.now().Add(UploadTTL)
		slot.UploadURL = s.Store.Presign("PUT", key, UploadTTL, s.now())
		out = append(out, slot)
	}
	return out, nil
}

func objectKey(accountID, cardID, side, photoID string) string {
	return "a/" + accountID + "/" + cardID + "/" + side + "-" + photoID + ".jpg"
}

// Confirmed reports what happened to one confirmed photo.
type Confirmed struct {
	PhotoID     string `json:"photo_id"`
	Status      string `json:"status"` // ready, missing, not_found
	CardVersion int    `json:"card_version,omitempty"`
}

// Confirm checks each upload really landed and points the card at it.
func (s *Service) Confirm(ctx context.Context, d cloudsync.Device, photoIDs []string) ([]Confirmed, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(photoIDs) > MaxPerCall {
		return nil, ErrTooMany
	}
	// Check the bucket for every upload at once (each HEAD is a round trip
	// to R2), then record the results one by one.
	type found struct {
		size int64
		ok   bool
		err  error
	}
	heads := make([]found, len(photoIDs))
	keys := make([]string, len(photoIDs))
	for i, id := range photoIDs {
		s.DB.QueryRowContext(ctx, `SELECT object_key FROM sync_photos WHERE id::text = $1 AND account_id = $2 AND status <> 'ready'`,
			strings.ToLower(id), d.AccountID).Scan(&keys[i])
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for i := range photoIDs {
		if keys[i] == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			size, ok, err := s.Store.Head(ctx, keys[i])
			heads[i] = found{size, ok, err}
		}(i)
	}
	wg.Wait()

	out := make([]Confirmed, 0, len(photoIDs))
	for i, id := range photoIDs {
		res := Confirmed{PhotoID: id}
		var key, cardID, side, status string
		err := s.DB.QueryRowContext(ctx, `SELECT object_key, card_id, side, status FROM sync_photos
			WHERE id::text = $1 AND account_id = $2`, strings.ToLower(id), d.AccountID).Scan(&key, &cardID, &side, &status)
		if errors.Is(err, sql.ErrNoRows) {
			res.Status = "not_found"
			out = append(out, res)
			continue
		}
		if err != nil {
			return nil, err
		}
		if status == "ready" {
			res.Status = "ready"
			out = append(out, res)
			continue
		}
		h := heads[i]
		if key != keys[i] { // not checked above (it changed in between): check now
			h.size, h.ok, h.err = s.Store.Head(ctx, key)
		}
		if h.err != nil {
			return nil, h.err
		}
		size := h.size
		if !h.ok || size <= 0 || size > MaxPhotoBytes {
			res.Status = "missing"
			out = append(out, res)
			continue
		}
		v, err := s.attach(ctx, d, id, cardID, side, size)
		if err != nil {
			return nil, err
		}
		res.Status, res.CardVersion = "ready", v
		out = append(out, res)
	}
	return out, nil
}

// attach marks a photo ready, retires the photo it replaces and sets the
// card's <side>_photo_id through the sync feed.
func (s *Service) attach(ctx context.Context, d cloudsync.Device, photoID, cardID, side string, size int64) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE sync_photos SET status = 'replaced'
		WHERE account_id = $1 AND card_id = $2 AND side = $3 AND status = 'ready' AND id <> $4`,
		d.AccountID, cardID, side, photoID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_photos SET status = 'ready', bytes = $2, confirmed_at = $3 WHERE id = $1`,
		photoID, size, s.now()); err != nil {
		return 0, err
	}
	val, _ := json.Marshal(photoID)
	v, err := s.Sync.ServerSet(ctx, tx, d.AccountID, "card", cardID, d.ID, map[string]json.RawMessage{side + "_photo_id": val})
	if err != nil {
		return 0, err
	}
	return v, tx.Commit()
}

// ViewURL returns a short-lived link to look at one of the account's photos.
func (s *Service) ViewURL(ctx context.Context, accountID, photoID string) (string, error) {
	if !s.Enabled() {
		return "", ErrNotConfigured
	}
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT object_key FROM sync_photos WHERE id::text = $1 AND account_id = $2 AND status = 'ready'`,
		strings.ToLower(photoID), accountID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.Store.Presign("GET", key, ViewTTL, s.now()), nil
}

// ErrNoHosting: the account doesn't have the web add-on, which is what
// includes hosted photo links for Shopify and Whatnot.
var ErrNoHosting = errors.New("hosted photo links come with the FireCardManager Web add-on")

// PublicPhoto opens a card photo for its permanent public link
// (/v1/p/<photo id>.jpg), used as the image address in Shopify and Whatnot
// import files. Only front and back photos, and only for accounts with the
// web add-on or its free trial (or admins). The photo id is a random UUID, so the link can't
// be guessed.
func (s *Service) PublicPhoto(ctx context.Context, photoID string) (io.ReadCloser, int64, error) {
	if !s.Enabled() {
		return nil, 0, ErrNotConfigured
	}
	if !uuidRE.MatchString(photoID) {
		return nil, 0, ErrNotFound
	}
	var key string
	var hosted bool
	err := s.DB.QueryRowContext(ctx, `SELECT p.object_key, (coalesce(a.web_addon_until > now(), false) OR a.sync_trial_ends_at > now() OR a.is_admin) AND a.status = 'active'
		FROM sync_photos p JOIN accounts a ON a.id = p.account_id
		WHERE p.id = $1 AND p.status = 'ready' AND p.side IN ('front', 'back')`, photoID).Scan(&key, &hosted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	if !hosted {
		return nil, 0, ErrNoHosting
	}
	body, size, found, err := s.Store.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, ErrNotFound
	}
	return body, size, nil
}

// Cleanup deletes objects that were replaced, and forgets uploads that were
// never confirmed within a day. Run periodically.
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, object_key FROM sync_photos
		WHERE status = 'replaced' OR (status = 'pending' AND created_at < $1) LIMIT 500`, s.now().Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	type item struct{ id, key string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.key); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	n := 0
	for _, it := range items {
		if err := s.Store.Delete(ctx, it.key); err != nil {
			return n, err
		}
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM sync_photos WHERE id = $1`, it.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Browser links: made from the start of the current hour and good for two
// hours, so a photo gets the same link all hour and the browser shows its
// saved copy instead of downloading it again. That keeps storage reads (what
// R2 charges for) down; R2 doesn't charge for the bytes themselves.
const (
	browserBucket = time.Hour
	browserTTL    = 2 * time.Hour
)

// BrowserURLs signs links for the website to show the given photos of the
// account's cards. Photos that aren't ready (or aren't the account's) are
// left out.
func (s *Service) BrowserURLs(ctx context.Context, accountID string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if !s.Enabled() || len(ids) == 0 {
		return out, nil
	}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if uuidRE.MatchString(id) {
			clean = append(clean, strings.ToLower(id))
		}
	}
	if len(clean) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text, object_key FROM sync_photos
		WHERE account_id = $1 AND status = 'ready' AND id = ANY($2::uuid[])`, accountID, pq.Array(clean))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	start := s.now().UTC().Truncate(browserBucket)
	extra := url.Values{"response-cache-control": {"private, max-age=3600, immutable"}}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		out[id] = s.Store.PresignWith("GET", key, browserTTL, start, extra)
	}
	return out, rows.Err()
}

// Read returns a ready photo's bytes (for the server to pass on, e.g. to a
// marketplace the seller lists on from the website).
func (s *Service) Read(ctx context.Context, accountID, photoID string) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT object_key FROM sync_photos WHERE id::text = $1 AND account_id = $2 AND status = 'ready'`,
		strings.ToLower(photoID), accountID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	body, _, found, err := s.Store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, MaxPhotoBytes+1))
}
