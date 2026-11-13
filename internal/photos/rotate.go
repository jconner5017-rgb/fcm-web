package photos

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"strings"

	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
)

const thumbSide = 400 // px, long side, like the desktop's thumbnails

// Rotate turns a card's front or back photo a quarter turn (clockwise when
// cw) and saves it, with a new thumbnail, as the card's photo. The desktop
// downloads the new photo on its next sync. Like any new photo, it's sent
// to Slabs & Singles again on the next listing update.
func (s *Service) Rotate(ctx context.Context, d cloudsync.Device, cardID, side string, cw bool) error {
	if !s.Enabled() {
		return ErrNotConfigured
	}
	if side != "front" && side != "back" {
		return ErrNotFound
	}
	cardID = strings.ToLower(cardID)
	var photoID string
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM sync_photos WHERE account_id = $1 AND card_id::text = $2 AND side = $3 AND status = 'ready'
		ORDER BY confirmed_at DESC NULLS LAST LIMIT 1`, d.AccountID, cardID, side).Scan(&photoID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	raw, err := s.Read(ctx, d.AccountID, photoID)
	if err != nil {
		return err
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	turned := rotate90(src, cw)
	full, err := encodeJPEG(turned)
	if err != nil {
		return err
	}
	thumb, err := encodeJPEG(shrink(turned, thumbSide))
	if err != nil {
		return err
	}
	if err := s.saveServerPhoto(ctx, d, cardID, side, full); err != nil {
		return err
	}
	return s.saveServerPhoto(ctx, d, cardID, side+"_thumb", thumb)
}

// saveServerPhoto stores a photo the server made and makes it the card's photo for that side.
func (s *Service) saveServerPhoto(ctx context.Context, d cloudsync.Device, cardID, side string, data []byte) error {
	var id string
	if err := s.DB.QueryRowContext(ctx, `INSERT INTO sync_photos (account_id, card_id, side, object_key, bytes)
		VALUES ($1, $2, $3, 'pending-' || gen_random_uuid(), $4) RETURNING id`, d.AccountID, cardID, side, len(data)).Scan(&id); err != nil {
		return err
	}
	key := objectKey(d.AccountID, cardID, side, id)
	if _, err := s.DB.ExecContext(ctx, `UPDATE sync_photos SET object_key = $2 WHERE id = $1`, id, key); err != nil {
		return err
	}
	if err := s.Store.Put(ctx, key, "image/jpeg", data); err != nil {
		return err
	}
	_, err := s.attach(ctx, d, id, cardID, side, int64(len(data)))
	return err
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes(), err
}

func rotate90(src image.Image, cw bool) image.Image {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(sb.Min.X+x, sb.Min.Y+y)
			if cw {
				dst.Set(h-1-y, x, c)
			} else {
				dst.Set(y, w-1-x, c)
			}
		}
	}
	return dst
}

// shrink scales so the long side is at most max px (box averaging).
func shrink(src image.Image, max int) image.Image {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= max {
		return src
	}
	nw, nh := w*max/long, h*max/long
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, (y+1)*h/nh
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, (x+1)*w/nw
			var r, g, b, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, _ := src.At(sb.Min.X+xx, sb.Min.Y+yy).RGBA()
					r, g, b, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), n+1
				}
			}
			if n == 0 {
				n = 1
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(b / n), 0xffff})
		}
	}
	return dst
}

