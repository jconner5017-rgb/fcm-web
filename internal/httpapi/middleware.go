package httpapi

import (
	"context"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/devices"
)

type ctxKey int

const (
	sessionKey ctxKey = iota + 1
	tokenKey
	deviceKey
)

func currentSession(r *http.Request) (auth.Session, bool) {
	s, ok := r.Context().Value(sessionKey).(auth.Session)
	return s, ok
}

func sessionToken(r *http.Request) string {
	t, _ := r.Context().Value(tokenKey).(string)
	return t
}

func currentDevice(r *http.Request) devices.Device {
	d, _ := r.Context().Value(deviceKey).(devices.Device)
	return d
}

// recoverer turns a panic into a 500 instead of crashing the server.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// requestLogger logs one line per request (never the query string of a
// link-finish or token call, which could carry secrets: those are POSTs).
func requestLogger(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "query", clip(r.URL.RawQuery, 200),
			"status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

// requireJSONForWrites rejects state-changing requests that aren't
// application/json. A browser can't send a cross-site JSON request without
// a CORS preflight, so together with SameSite cookies this blocks CSRF.
// Device calls carry a bearer token, which can't be forged cross-site.
func requireJSONForWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Method == http.MethodDelete || (bearer && r.ContentLength == 0) {
				break
			}
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "json_required", "Content-Type must be application/json", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loadSession attaches the signed-in account, if the cookie is valid.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			if sess, err := s.auth.SessionFor(r.Context(), c.Value); err == nil {
				ctx := context.WithValue(r.Context(), sessionKey, sess)
				r = r.WithContext(context.WithValue(ctx, tokenKey, c.Value))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireAccount allows a fully signed-in account (2FA step done).
func requireAccount(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := currentSession(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in first.", nil)
			return
		}
		if !sess.MFAOK {
			writeError(w, http.StatusUnauthorized, "mfa_required", "Enter the code from your authenticator app.", nil)
			return
		}
		next(w, r)
	}
}

// requireDevice checks a desktop app's bearer token.
func (s *Server) requireDevice(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "device_unlinked", "This PC isn't linked. Link it from Settings > Cloud Sync.", nil)
			return
		}
		d, err := s.devices.ByToken(r.Context(), strings.TrimSpace(tok))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "device_unlinked", "This PC was unlinked. Link it again from Settings > Cloud Sync.", nil)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), deviceKey, d)))
	}
}

// clientIP returns the caller's IP. Behind Render's proxy every request
// arrives from the proxy, so with trustProxy the right-most address in
// X-Forwarded-For that isn't one of our own hops is used. Only for rate
// limiting and logging, never access decisions.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(hops[i]))
			if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimiter allows `limit` events per key per window (fixed window,
// in memory, per instance).
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*windowCount{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	wc, ok := l.hits[key]
	if !ok || now.Sub(wc.start) >= l.window {
		if len(l.hits) > 100_000 {
			l.hits = map[string]*windowCount{}
		}
		l.hits[key] = &windowCount{start: now, n: 1}
		return true
	}
	if wc.n >= l.limit {
		return false
	}
	wc.n++
	return true
}
