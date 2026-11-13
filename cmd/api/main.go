// Command api runs the FCM Web API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/db"
	"github.com/jconner5017-rgb/fcm-web/internal/devices"
	"github.com/jconner5017-rgb/fcm-web/internal/email"
	"github.com/jconner5017-rgb/fcm-web/internal/httpapi"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func run(log *slog.Logger) error {
	dbURL := env("DATABASE_URL", "")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	appEnv := env("APP_ENV", "dev")
	port := env("PORT", "8080")
	webURL := env("WEB_URL", "http://localhost:3000")
	ttlDays, _ := strconv.Atoi(env("SESSION_DAYS", "30"))
	if ttlDays <= 0 {
		ttlDays = 30
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return err
	}
	log.Info("migrations applied")

	var admins []string
	for _, a := range strings.Split(env("ADMIN_EMAILS", ""), ",") {
		if a = strings.TrimSpace(a); a != "" {
			admins = append(admins, a)
		}
	}
	authSvc := &auth.Service{DB: database, SessionTTL: time.Duration(ttlDays) * 24 * time.Hour, AdminEmails: admins}
	if err := authSvc.PromoteAdmins(ctx); err != nil {
		log.Error("promote admins", "err", err)
	}

	mfaKey, err := auth.ParseKey(os.Getenv("MFA_ENCRYPTION_KEY"))
	if err != nil {
		return err
	}
	if mfaKey == nil {
		log.Warn("MFA_ENCRYPTION_KEY not set: two-factor sign-in can't be turned on")
	}

	sender, err := email.Pick(os.Getenv("EMAIL_PROVIDER"), os.Getenv("EMAIL_API_KEY"), os.Getenv("EMAIL_API_SECRET"))
	if err != nil {
		return err
	}
	mailSvc := &email.Service{DB: database, Sender: sender, WebURL: webURL,
		From: env("EMAIL_FROM", "FireCardManager <no-reply@firecardmanager.com>"), Warn: log.Warn}
	if sender == nil {
		log.Warn("EMAIL_PROVIDER not set: verification and reset emails are not sent")
	}

	srv := httpapi.New(httpapi.Options{
		DB: database, Auth: authSvc, MFA: &auth.MFA{DB: database, Key: mfaKey}, Mail: mailSvc,
		Devices: &devices.Service{DB: database}, Sync: &cloudsync.Service{DB: database},
		Log: log, CookieSecure: appEnv != "dev", SessionTTL: authSvc.SessionTTL,
		TrustProxy: env("TRUST_PROXY_HEADERS", "false") == "true", WebURL: webURL,
	})

	// Background jobs: send queued email every 15 s, tidy sessions hourly.
	go func() {
		mailTick := time.NewTicker(15 * time.Second)
		tidy := time.NewTicker(time.Hour)
		defer mailTick.Stop()
		defer tidy.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-mailTick.C:
				if n, err := mailSvc.RunOutbox(ctx, 50); err != nil {
					log.Warn("email outbox", "err", err)
				} else if n > 0 {
					log.Info("emails sent", "count", n)
				}
			case <-tidy.C:
				if _, err := authSvc.PurgeExpiredSessions(ctx); err != nil {
					log.Warn("purge sessions", "err", err)
				}
				database.ExecContext(ctx, `DELETE FROM device_links WHERE expires_at < now() - interval '1 day'`)
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "port", port, "env", appEnv)
		errCh <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
