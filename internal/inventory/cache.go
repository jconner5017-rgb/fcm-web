package inventory

import (
	"context"
	"sync"
	"time"
)

// Home-page cache (2026-11-19): the summary and dashboard read every synced
// card, and with a few thousand cards that took 5-8 s on the small database.
// Their results are kept per account until the account's sync change log
// moves on (every card, box or sold edit adds a row to sync_changes, from the
// PC or the website), or for 10 minutes at most.

const cacheTTL = 10 * time.Minute

type cacheEntry struct {
	version int64
	at      time.Time
	val     any
}

type resultCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

func (c *resultCache) get(key string, version int64) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || e.version != version || time.Since(e.at) > cacheTTL {
		return nil, false
	}
	return e.val, true
}

func (c *resultCache) put(key string, version int64, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 2000 {
		c.m = map[string]cacheEntry{}
	}
	c.m[key] = cacheEntry{version: version, at: time.Now(), val: val}
}

// changeVersion is the account's newest sync change (an index lookup).
func (s *Service) changeVersion(ctx context.Context, accountID string) (int64, error) {
	var v int64
	err := s.DB.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM sync_changes WHERE account_id = $1`, accountID).Scan(&v)
	return v, err
}

// cached returns the stored result for key while the account hasn't changed,
// or computes and stores it. A failed version lookup just computes.
func cached[T any](ctx context.Context, s *Service, accountID, key string, compute func() (T, error)) (T, error) {
	v, verr := s.changeVersion(ctx, accountID)
	if verr == nil {
		if val, ok := s.cache.get(accountID+"|"+key, v); ok {
			return val.(T), nil
		}
	}
	out, err := compute()
	if err == nil && verr == nil {
		s.cache.put(accountID+"|"+key, v, out)
	}
	return out, err
}
