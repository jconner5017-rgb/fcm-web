// Package httpapi is FCM Web's JSON API: accounts for the website, linking
// for the desktop apps, and the sync endpoints those apps call.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/devices"
	"github.com/jconner5017-rgb/fcm-web/internal/email"
	"github.com/jconner5017-rgb/fcm-web/internal/inventory"
	"github.com/jconner5017-rgb/fcm-web/internal/photos"
)

const sessionCookie = "fcm_session"

type Options struct {
	DB           *sql.DB
	Auth         *auth.Service
	MFA          *auth.MFA
	Mail         *email.Service
	Devices      *devices.Service
	Sync         *cloudsync.Service
	Photos       *photos.Service
	Log          *slog.Logger
	CookieSecure bool
	SessionTTL   time.Duration
	TrustProxy   bool
	WebURL       string // https://app.firecardmanager.com, for links in email
	Now          func() time.Time
}

type Server struct {
	db           *sql.DB
	auth         *auth.Service
	mfa          *auth.MFA
	mail         *email.Service
	devices      *devices.Service
	sync         *cloudsync.Service
	photos       *photos.Service
	inv          *inventory.Service
	log          *slog.Logger
	cookieSecure bool
	sessionTTL   time.Duration
	trustProxy   bool
	webURL       string
	now          func() time.Time

	authLimiter *rateLimiter // sign-in, sign-up, codes: per IP
	linkLimiter *rateLimiter // desktop link starts: per IP
	mailLimiter *rateLimiter // reset and verify emails: per IP
}

func New(o Options) *Server {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	return &Server{
		db: o.DB, auth: o.Auth, mfa: o.MFA, mail: o.Mail, devices: o.Devices, sync: o.Sync, photos: o.Photos, inv: &inventory.Service{DB: o.DB, Sync: o.Sync, Photos: o.Photos}, log: o.Log,
		cookieSecure: o.CookieSecure, sessionTTL: o.SessionTTL, trustProxy: o.TrustProxy,
		webURL: strings.TrimRight(o.WebURL, "/"), now: now,
		authLimiter: newRateLimiter(20, time.Minute),
		linkLimiter: newRateLimiter(10, time.Minute),
		mailLimiter: newRateLimiter(5, 10*time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Accounts (website)
	mux.HandleFunc("POST /v1/auth/signup", s.limited(s.authLimiter, s.handleSignup))
	mux.HandleFunc("POST /v1/auth/login", s.limited(s.authLimiter, s.handleLogin))
	mux.HandleFunc("POST /v1/auth/mfa", s.limited(s.authLimiter, s.handleLoginMFA))
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /v1/auth/verify-email", s.handleVerifyEmail)
	mux.HandleFunc("POST /v1/auth/forgot-password", s.limited(s.mailLimiter, s.handleForgotPassword))
	mux.HandleFunc("POST /v1/auth/reset-password", s.limited(s.authLimiter, s.handleResetPassword))
	mux.HandleFunc("GET /v1/me", requireAccount(s.handleMe))
	mux.HandleFunc("POST /v1/me/resend-verification", requireAccount(s.limited(s.mailLimiter, s.handleResendVerification)))
	mux.HandleFunc("POST /v1/me/password", requireAccount(s.limited(s.authLimiter, s.handleChangePassword)))
	mux.HandleFunc("POST /v1/me/mfa/setup", requireAccount(s.handleMFASetup))
	mux.HandleFunc("POST /v1/me/mfa/confirm", requireAccount(s.limited(s.authLimiter, s.handleMFAConfirm)))
	mux.HandleFunc("POST /v1/me/mfa/disable", requireAccount(s.limited(s.authLimiter, s.handleMFADisable)))

	// Linking a desktop app
	mux.HandleFunc("POST /v1/devices/link/start", s.limited(s.linkLimiter, s.handleLinkStart))
	mux.HandleFunc("POST /v1/devices/link/finish", s.handleLinkFinish)
	mux.HandleFunc("GET /v1/devices/link/{code}", requireAccount(s.handleLinkLookup))
	mux.HandleFunc("POST /v1/devices/link/approve", requireAccount(s.handleLinkApprove))
	mux.HandleFunc("GET /v1/devices", requireAccount(s.handleDevices))
	mux.HandleFunc("DELETE /v1/devices/{id}", requireAccount(s.handleDeviceRevoke))

	// Sync (desktop apps, device token)
	mux.HandleFunc("GET /v1/sync/status", s.requireDevice(s.handleSyncStatus))
	mux.HandleFunc("POST /v1/sync/push", s.requireDevice(s.handleSyncPush))
	mux.HandleFunc("GET /v1/sync/pull", s.requireDevice(s.handleSyncPull))
	mux.HandleFunc("POST /v1/photos/upload-urls", s.requireDevice(s.handlePhotoUploadURLs))
	mux.HandleFunc("POST /v1/photos/confirm", s.requireDevice(s.handlePhotoConfirm))
	mux.HandleFunc("GET /v1/photos/{id}", s.handlePhotoView) // website (session) or desktop (device token)
	mux.HandleFunc("GET /v1/p/{file}", s.handlePublicPhoto)  // permanent link for Shopify/Whatnot imports (add-on)

	// Admin (owner, with 2FA on)
	// Inventory for the website (read-only for now).
	mux.HandleFunc("GET /v1/inventory/summary", requireAccount(s.handleInvSummary))
	mux.HandleFunc("GET /v1/inventory/cards", requireAccount(s.handleInvCards))
	mux.HandleFunc("GET /v1/inventory/cards/{id}", requireAccount(s.handleInvCard))
	mux.HandleFunc("GET /v1/inventory/sold", requireAccount(s.handleInvSold))
	mux.HandleFunc("GET /v1/inventory/boxes", requireAccount(s.handleInvBoxes))
	mux.HandleFunc("PATCH /v1/inventory/cards/{id}", requireAccount(s.handleInvEdit))
	mux.HandleFunc("POST /v1/inventory/cards/{id}/sold", requireAccount(s.handleInvMarkSold))
	mux.HandleFunc("GET /v1/inventory/conflicts", requireAccount(s.handleInvConflicts))
	mux.HandleFunc("POST /v1/inventory/conflicts/{id}", requireAccount(s.handleInvResolve))

	mux.HandleFunc("GET /v1/admin/accounts", s.requireAdmin(s.handleAdminAccounts))
	mux.HandleFunc("POST /v1/admin/accounts/{id}/addon", s.requireAdmin(s.handleAdminAddon))
	mux.HandleFunc("POST /v1/admin/accounts/{id}/trial", s.requireAdmin(s.handleAdminTrial))

	var h http.Handler = mux
	h = s.loadSession(h)
	h = requireJSONForWrites(h)
	h = requestLogger(s.log, h)
	h = recoverer(s.log, h)
	return h
}

func (s *Server) limited(l *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r, s.trustProxy) + " " + r.URL.Path) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many tries. Wait a few minutes and try again.", nil)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db_down", "database unreachable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Something went wrong. Try again in a minute.", nil)
}

func validationError(w http.ResponseWriter, err error) bool {
	var ve *auth.ValidationError
	if errors.As(err, &ve) {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Check the highlighted fields.", ve.Fields)
		return true
	}
	return false
}

// ---------------- accounts ----------------

type meResponse struct {
	Account     auth.Account `json:"account"`
	MFAEnabled  bool         `json:"mfa_enabled"`
	SyncAllowed bool         `json:"sync_allowed"`
	Editions    []string     `json:"editions"`
	CardLimit   int          `json:"card_limit"`
}

func (s *Server) me(ctx context.Context, a auth.Account) (meResponse, error) {
	st, err := s.mfa.Status(ctx, a.ID)
	if err != nil {
		return meResponse{}, err
	}
	eds, err := s.devices.Editions(ctx, a.ID)
	if err != nil {
		return meResponse{}, err
	}
	return meResponse{Account: a, MFAEnabled: st != "setup_required", SyncAllowed: a.SyncAllowed(s.now()),
		Editions: eds, CardLimit: cloudsync.CardLimit}, nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	out, err := s.me(r.Context(), sess.Account)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	a, err := s.auth.Signup(r.Context(), in.Email, in.Password)
	if validationError(w, err) {
		return
	}
	if errors.Is(err, auth.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "email_taken", "An account with that email already exists. Sign in instead.",
			map[string]string{"email": "Already registered."})
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	token, err := s.auth.StartSession(r.Context(), a.ID, clientIP(r, s.trustProxy), r.UserAgent())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.sendVerification(r.Context(), a)
	s.setSessionCookie(w, token, s.sessionTTL)
	out, err := s.me(r.Context(), a)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) sendVerification(ctx context.Context, a auth.Account) {
	if !s.mail.Enabled() {
		return
	}
	tok, err := s.mail.NewToken(ctx, s.db, a.ID, a.Email, "verify", 48*time.Hour)
	if err != nil {
		s.log.Warn("verify token", "err", err)
		return
	}
	body := "Welcome to FireCardManager Web.\n\nConfirm your email address:\n" + s.webURL + "/verify-email?token=" + tok +
		"\n\nThe link works for 48 hours. If you didn't sign up, ignore this email."
	if err := s.mail.Queue(ctx, s.db, a.ID, a.Email, "Confirm your FireCardManager email", body); err != nil {
		s.log.Warn("queue verify email", "err", err)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	a, token, mfaNeeded, err := s.auth.Login(r.Context(), in.Email, in.Password, clientIP(r, s.trustProxy), r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "bad_credentials", "Wrong email or password.", nil)
		return
	case errors.Is(err, auth.ErrAccountDisabled):
		writeError(w, http.StatusForbidden, "disabled", "This account is disabled. Contact FireCardManager support.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	if mfaNeeded {
		s.setSessionCookie(w, token, 10*time.Minute)
		writeJSON(w, http.StatusOK, map[string]any{"mfa_required": true})
		return
	}
	s.setSessionCookie(w, token, s.sessionTTL)
	out, err := s.me(r.Context(), a)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleLoginMFA finishes a sign-in with an authenticator or backup code.
func (s *Server) handleLoginMFA(w http.ResponseWriter, r *http.Request) {
	sess, ok := currentSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Your sign-in timed out. Enter your password again.", nil)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.mfa.Verify(r.Context(), sess.Account.ID, in.Code)
	if errors.Is(err, auth.ErrBadCode) || errors.Is(err, auth.ErrMFALocked) {
		writeError(w, http.StatusUnauthorized, "bad_code", err.Error(), nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.auth.MarkSessionMFA(r.Context(), sessionToken(r)); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.setSessionCookie(w, sessionToken(r), s.sessionTTL)
	out, err := s.me(r.Context(), sess.Account)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"me": out, "used_backup": res.UsedBackup, "backups_left": res.BackupsLeft})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if t := sessionToken(r); t != "" {
		s.auth.Logout(r.Context(), t)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer tx.Rollback()
	id, addr, err := s.mail.UseToken(r.Context(), tx, in.Token, "verify")
	if errors.Is(err, email.ErrToken) {
		writeError(w, http.StatusBadRequest, "bad_token", "That link has expired or was already used.", nil)
		return
	}
	if err == nil {
		err = s.auth.MarkVerified(r.Context(), tx, id, addr)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"verified": true})
}

func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	if sess.Account.EmailVerified {
		writeJSON(w, http.StatusOK, map[string]bool{"sent": false})
		return
	}
	s.sendVerification(r.Context(), sess.Account)
	writeJSON(w, http.StatusOK, map[string]bool{"sent": s.mail.Enabled()})
}

// handleForgotPassword always answers the same way, so it can't be used to
// find out which emails have accounts.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if a, err := s.auth.ByEmail(r.Context(), in.Email); err == nil && a.Status == "active" && s.mail.Enabled() {
		if n, _ := s.mail.RecentTokens(r.Context(), a.ID, "reset", time.Hour); n < 3 {
			if tok, err := s.mail.NewToken(r.Context(), s.db, a.ID, a.Email, "reset", time.Hour); err == nil {
				body := "Someone asked to reset the password for your FireCardManager Web account.\n\nChoose a new password:\n" +
					s.webURL + "/reset-password?token=" + tok + "\n\nThe link works for one hour. If this wasn't you, ignore this email; your password hasn't changed."
				s.mail.Queue(r.Context(), s.db, a.ID, a.Email, "Reset your FireCardManager password", body)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer tx.Rollback()
	id, addr, err := s.mail.UseToken(r.Context(), tx, in.Token, "reset")
	if errors.Is(err, email.ErrToken) {
		writeError(w, http.StatusBadRequest, "bad_token", "That link has expired or was already used. Ask for a new one.", nil)
		return
	}
	if err == nil {
		err = s.auth.SetPassword(r.Context(), tx, id, in.Password)
	}
	if validationError(w, err) {
		return
	}
	if err == nil {
		// Getting the reset email proves the address too.
		err = s.auth.MarkVerified(r.Context(), tx, id, addr)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE account_id = $1`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	err := s.auth.ChangePassword(r.Context(), sess.Account.ID, in.Current, in.Password)
	if validationError(w, err) {
		return
	}
	if err == nil {
		err = s.auth.LogoutOthers(r.Context(), sess.Account.ID, sessionToken(r))
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	secret, link, err := s.mfa.Setup(r.Context(), sess.Account.ID, sess.Account.Email)
	switch {
	case errors.Is(err, auth.ErrMFAEnrolled):
		writeError(w, http.StatusConflict, "mfa_enabled", "Two-factor sign-in is already on.", nil)
		return
	case errors.Is(err, auth.ErrMFANotConfigured):
		writeError(w, http.StatusServiceUnavailable, "mfa_unavailable", "Two-factor sign-in isn't available yet.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_url": link})
}

func (s *Server) handleMFAConfirm(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	var in struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	codes, err := s.mfa.Confirm(r.Context(), sess.Account.ID, in.Code)
	switch {
	case errors.Is(err, auth.ErrBadCode), errors.Is(err, auth.ErrMFANotEnrolled), errors.Is(err, auth.ErrMFAEnrolled):
		writeError(w, http.StatusBadRequest, "bad_code", err.Error(), nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup_codes": codes})
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	var in struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := s.mfa.Verify(r.Context(), sess.Account.ID, in.Code); err != nil {
		if errors.Is(err, auth.ErrBadCode) || errors.Is(err, auth.ErrMFALocked) || errors.Is(err, auth.ErrMFANotEnrolled) {
			writeError(w, http.StatusBadRequest, "bad_code", err.Error(), nil)
			return
		}
		s.internalError(w, r, err)
		return
	}
	if err := s.mfa.Reset(r.Context(), sess.Account.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- linking a desktop app ----------------

func (s *Server) handleLinkStart(w http.ResponseWriter, r *http.Request) {
	var in devices.StartInput
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.devices.Start(r.Context(), in)
	switch {
	case errors.Is(err, devices.ErrBadInput):
		writeError(w, http.StatusBadRequest, "bad_input", "The app sent an incomplete link request. Update FireCardManager and try again.", nil)
		return
	case errors.Is(err, devices.ErrBadLicense):
		writeError(w, http.StatusForbidden, "bad_license", "This PC's license key didn't check out. Re-enter it under Help > License, then try again.", nil)
		return
	case errors.Is(err, devices.ErrExpired):
		writeError(w, http.StatusForbidden, "license_expired", "This license has expired. Renew it to link this PC.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"link_id": res.LinkID, "user_code": res.UserCode, "poll_secret": res.PollSecret,
		"expires_at": res.ExpiresAt, "interval": res.Interval,
		"verify_url": s.webURL + "/link?code=" + res.UserCode,
	})
}

func (s *Server) handleLinkFinish(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LinkID     string `json:"link_id"`
		PollSecret string `json:"poll_secret"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.devices.Finish(r.Context(), in.LinkID, in.PollSecret)
	switch {
	case errors.Is(err, devices.ErrPending):
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	case errors.Is(err, devices.ErrCodeNotFound):
		writeError(w, http.StatusGone, "link_expired", "The link code expired. Start again.", nil)
		return
	case errors.Is(err, devices.ErrCodeUsed):
		writeError(w, http.StatusGone, "link_used", "This link was already finished. Start again if this PC isn't linked.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLinkLookup(w http.ResponseWriter, r *http.Request) {
	p, err := s.devices.Lookup(r.Context(), r.PathValue("code"))
	switch {
	case errors.Is(err, devices.ErrCodeNotFound):
		writeError(w, http.StatusNotFound, "code_not_found", "That code wasn't found or has expired. Start again from the desktop app.", nil)
		return
	case errors.Is(err, devices.ErrCodeUsed):
		writeError(w, http.StatusGone, "code_used", "That code was already approved.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleLinkApprove(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	var in struct {
		UserCode string `json:"user_code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	err := s.devices.Approve(r.Context(), sess.Account.ID, in.UserCode)
	switch {
	case errors.Is(err, devices.ErrCodeNotFound):
		writeError(w, http.StatusNotFound, "code_not_found", "That code wasn't found or has expired. Start again from the desktop app.", nil)
		return
	case errors.Is(err, devices.ErrCodeUsed):
		writeError(w, http.StatusGone, "code_used", "That code was already approved.", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"approved": true})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	list, err := s.devices.List(r.Context(), sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": list})
}

func (s *Server) handleDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	err := s.devices.Revoke(r.Context(), sess.Account.ID, r.PathValue("id"))
	if errors.Is(err, devices.ErrNoDevice) {
		writeError(w, http.StatusNotFound, "not_found", "That PC isn't linked.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------- sync ----------------

// syncDevice loads the device's account and checks it may sync.
func (s *Server) syncDevice(w http.ResponseWriter, r *http.Request, needAllowed bool) (cloudsync.Device, auth.Account, bool) {
	d := currentDevice(r)
	a, err := s.auth.ByID(r.Context(), d.AccountID)
	if err != nil {
		s.internalError(w, r, err)
		return cloudsync.Device{}, auth.Account{}, false
	}
	if a.Status != "active" {
		writeError(w, http.StatusForbidden, "disabled", "This FireCardManager Web account is disabled.", nil)
		return cloudsync.Device{}, auth.Account{}, false
	}
	if needAllowed && !a.SyncAllowed(s.now()) {
		writeError(w, http.StatusPaymentRequired, "sync_inactive",
			"Cloud sync is off: the free trial or the FireCardManager Web add-on has ended. The add-on is $20 with a 6-month FCM license or $40 with a 12-month one. The desktop app keeps working either way.", nil)
		return cloudsync.Device{}, auth.Account{}, false
	}
	return cloudsync.Device{ID: d.ID, AccountID: d.AccountID, Edition: d.Edition}, a, true
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	d, a, ok := s.syncDevice(w, r, false)
	if !ok {
		return
	}
	st, err := s.sync.Status(r.Context(), d)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": st, "account_email": a.Email, "sync_allowed": a.SyncAllowed(s.now()),
		"trial_ends_at": a.SyncTrialEndsAt, "web_addon": a.AddonActive(s.now()), "web_addon_until": a.WebAddonUntil, "edition": d.Edition, "device_id": d.ID,
		// Hosted photo links for Shopify and Whatnot exports come with the add-on.
		"photo_links": (a.AddonActive(s.now()) || a.IsAdmin) && s.photos.Enabled(),
	})
}

func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	d, _, ok := s.syncDevice(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Changes []cloudsync.Change `json:"changes"`
	}
	if !decodeJSONMax(w, r, &in, 8<<20) {
		return
	}
	res, err := s.sync.Push(r.Context(), d, in.Changes)
	if errors.Is(err, cloudsync.ErrBatchTooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "batch_too_big", err.Error(), nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	d, _, ok := s.syncDevice(w, r, true)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := s.sync.Pull(r.Context(), d, after, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------------- admin ----------------

func (s *Server) handleAdminAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.auth.ListAccounts(r.Context(), r.URL.Query().Get("q"), 200)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list})
}

func (s *Server) handleAdminAddon(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Months int    `json:"months"` // 6 or 12; 0 ends the add-on now
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if in.Months == 0 {
		err := s.auth.EndWebAddon(r.Context(), id)
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Account not found.", nil)
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		s.log.Info("web add-on ended", "account", id)
		writeJSON(w, http.StatusOK, map[string]any{"web_addon_until": nil})
		return
	}
	until, err := s.auth.ExtendWebAddon(r.Context(), id, in.Months, in.Note)
	if validationError(w, err) {
		return
	}
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Account not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("web add-on extended", "account", id, "months", in.Months, "until", until)
	writeJSON(w, http.StatusOK, map[string]any{"web_addon_until": until})
}

func (s *Server) handleAdminTrial(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Until string `json:"until"` // YYYY-MM-DD
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	until, err := time.Parse("2006-01-02", in.Until)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Use a date like 2026-12-31.", map[string]string{"until": "Use YYYY-MM-DD."})
		return
	}
	if _, err := s.auth.ByID(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Account not found.", nil)
		return
	}
	if err := s.auth.ExtendTrial(r.Context(), r.PathValue("id"), until.Add(24*time.Hour-time.Second)); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------- photos ----------------

func (s *Server) photoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, photos.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "photos_off", "Photo upload isn't switched on yet. Cards still sync; photos follow later.", nil)
	case errors.Is(err, photos.ErrTooMany):
		writeError(w, http.StatusRequestEntityTooLarge, "batch_too_big", err.Error(), nil)
	case errors.Is(err, photos.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Photo not found.", nil)
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) handlePhotoUploadURLs(w http.ResponseWriter, r *http.Request) {
	d, _, ok := s.syncDevice(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Photos []photos.UploadRequest `json:"photos"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	slots, err := s.photos.UploadURLs(r.Context(), d, in.Photos)
	if err != nil {
		s.photoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploads": slots})
}

func (s *Server) handlePhotoConfirm(w http.ResponseWriter, r *http.Request) {
	d, _, ok := s.syncDevice(w, r, true)
	if !ok {
		return
	}
	var in struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.photos.Confirm(r.Context(), d, in.PhotoIDs)
	if err != nil {
		s.photoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": res})
}

// handlePhotoView sends the browser (or app) to a short-lived link for the
// photo. With ?format=json it answers {"url": ...} instead of redirecting.
func (s *Server) handlePhotoView(w http.ResponseWriter, r *http.Request) {
	var accountID string
	if sess, ok := currentSession(r); ok && sess.MFAOK {
		accountID = sess.Account.ID
	} else if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if d, err := s.devices.ByToken(r.Context(), strings.TrimSpace(tok)); err == nil {
			accountID = d.AccountID
		}
	}
	if accountID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in first.", nil)
		return
	}
	u, err := s.photos.ViewURL(r.Context(), accountID, r.PathValue("id"))
	if err != nil {
		s.photoError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, map[string]string{"url": u})
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// handlePublicPhoto serves /v1/p/<photo id>.jpg: the permanent photo link a
// Shopify or Whatnot import file points at. Those services fetch the image
// once and keep their own copy.
func (s *Server) handlePublicPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutSuffix(strings.ToLower(r.PathValue("file")), ".jpg")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Photo not found.", nil)
		return
	}
	body, size, err := s.photos.PublicPhoto(r.Context(), id)
	if errors.Is(err, photos.ErrNoHosting) {
		writeError(w, http.StatusForbidden, "no_hosting", err.Error(), nil)
		return
	}
	if err != nil {
		s.photoError(w, r, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	io.Copy(w, body)
}

// ---------------- inventory (website) ----------------

// invEdition is the edition the request asked for, or the account's main one.
func (s *Server) invEdition(r *http.Request, accountID string) (string, error) {
	eds, err := s.inv.Editions(r.Context(), accountID)
	if err != nil {
		return "", err
	}
	return inventory.PickEdition(eds, r.URL.Query().Get("edition")), nil
}

func (s *Server) handleInvSummary(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	sum, err := s.inv.Summary(r.Context(), sess.Account.ID, r.URL.Query().Get("edition"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleInvCards(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	ed, err := s.invEdition(r, sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	v := r.URL.Query()
	page, _ := strconv.Atoi(v.Get("page"))
	per, _ := strconv.Atoi(v.Get("per"))
	out, err := s.inv.Cards(r.Context(), sess.Account.ID, inventory.Query{
		Edition: ed, Box: v.Get("box"), Row: v.Get("row"), Status: v.Get("status"), Search: v.Get("q"),
		Sort: v.Get("sort"), Desc: v.Get("dir") == "desc", Page: page, PerPage: per,
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edition": ed, "cards": out.Cards, "total": out.Total, "page": out.Page, "per_page": out.PerPage})
}

func (s *Server) handleInvCard(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	c, err := s.inv.CardWithPhotos(r.Context(), sess.Account.ID, r.PathValue("id"))
	if errors.Is(err, inventory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "That card isn't in your online inventory.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleInvSold(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	ed, err := s.invEdition(r, sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	v := r.URL.Query()
	page, _ := strconv.Atoi(v.Get("page"))
	out, err := s.inv.Sold(r.Context(), sess.Account.ID, ed, v.Get("q"), page, 0)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edition": ed, "sold": out.Sold, "total": out.Total, "revenue": out.Revenue,
		"page": out.Page, "per_page": out.PerPage})
}

// invWriteAllowed: edits from the website are sync, so they need the trial
// or the web add-on, like a PC's sync does.
func (s *Server) invWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	sess, _ := currentSession(r)
	if !sess.Account.SyncAllowed(s.now()) {
		writeError(w, http.StatusPaymentRequired, "sync_off",
			"Editing on the website needs the FireCardManager Web add-on (your free trial has ended).", nil)
		return false
	}
	return true
}

func (s *Server) invEditError(w http.ResponseWriter, r *http.Request, err error) {
	var fe inventory.FieldErrors
	switch {
	case errors.As(err, &fe):
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Check the highlighted fields.", fe)
	case errors.Is(err, inventory.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "That card isn't in your online inventory.", nil)
	case errors.Is(err, inventory.ErrSoldAlready):
		writeError(w, http.StatusConflict, "sold", "This card is already sold.", nil)
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) handleInvBoxes(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	boxes, err := s.inv.Boxes(r.Context(), sess.Account.ID, r.URL.Query().Get("edition"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boxes": boxes})
}

func (s *Server) handleInvEdit(w http.ResponseWriter, r *http.Request) {
	if !s.invWriteAllowed(w, r) {
		return
	}
	var in struct {
		BaseVersion int                        `json:"base_version"`
		Fields      map[string]json.RawMessage `json:"fields"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	out, err := s.inv.EditCard(r.Context(), sess.Account.ID, r.PathValue("id"), in.BaseVersion, in.Fields)
	if err != nil {
		s.invEditError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleInvMarkSold(w http.ResponseWriter, r *http.Request) {
	if !s.invWriteAllowed(w, r) {
		return
	}
	var in struct {
		BaseVersion int `json:"base_version"`
		inventory.Sale
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	id, err := s.inv.MarkSold(r.Context(), sess.Account.ID, r.PathValue("id"), in.BaseVersion, in.Sale, inventory.NewID())
	if err != nil {
		s.invEditError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sold_id": id})
}

func (s *Server) handleInvConflicts(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	list, err := s.inv.Conflicts(r.Context(), sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conflicts": list})
}

func (s *Server) handleInvResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Use string `json:"use"` // "kept" or "lost"
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Use != "kept" && in.Use != "lost" {
		writeError(w, http.StatusBadRequest, "bad_choice", "Choose which value to keep.", nil)
		return
	}
	if in.Use == "lost" && !s.invWriteAllowed(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "That conflict is already resolved.", nil)
		return
	}
	sess, _ := currentSession(r)
	err = s.inv.ResolveConflict(r.Context(), sess.Account.ID, id, in.Use == "lost")
	if errors.Is(err, inventory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "That conflict is already resolved, or the card was deleted.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
