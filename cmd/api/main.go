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
	"github.com/jconner5017-rgb/fcm-web/internal/ebay"
	"github.com/jconner5017-rgb/fcm-web/internal/email"
	"github.com/jconner5017-rgb/fcm-web/internal/export"
	"github.com/jconner5017-rgb/fcm-web/internal/templates"
	"github.com/jconner5017-rgb/fcm-web/internal/httpapi"
	"github.com/jconner5017-rgb/fcm-web/internal/lister"
	"github.com/jconner5017-rgb/fcm-web/internal/photos"
	"github.com/jconner5017-rgb/fcm-web/internal/slabs"
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

	syncSvc := &cloudsync.Service{DB: database}
	photoSvc := &photos.Service{DB: database, Sync: syncSvc}
	if ep := env("R2_ENDPOINT", ""); ep != "" {
		photoSvc.Store = &photos.S3{Endpoint: ep, Bucket: env("R2_BUCKET", "fcm-photos"), Region: env("R2_REGION", "auto"),
			AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY")}
		log.Info("photo storage", "bucket", photoSvc.Store.Bucket)
	} else {
		log.Warn("R2_ENDPOINT not set: photo upload is off (cards still sync)")
	}

	listerSvc := &lister.Service{DB: database, Sync: syncSvc, Sealer: &auth.MFA{DB: database, Key: mfaKey}, Photos: photoSvc,
		Slabs: &slabs.Client{BaseURL: env("SLABS_API_URL", slabs.DefaultBaseURL)}, Log: log, Pause: 300 * time.Millisecond}
	go listerSvc.Run(ctx)

	ebaySvc := &ebay.Service{DB: database, Sealer: &auth.MFA{DB: database, Key: mfaKey},
		AppID: os.Getenv("EBAY_APP_ID"), CertID: os.Getenv("EBAY_CERT_ID"), RuName: os.Getenv("EBAY_RU_NAME")}
	if ebaySvc.Configured() && mfaKey != nil {
		log.Info("eBay connection on")
	} else {
		log.Warn("EBAY_APP_ID / EBAY_CERT_ID / EBAY_RU_NAME (and MFA_ENCRYPTION_KEY) not all set: eBay on the website is off")
	}

	listerSvc.Ebay, listerSvc.PhotoLink = ebaySvc, export.HostedLinks(webURL)
	listerSvc.Templates = &templates.Service{DB: database}

	srv := httpapi.New(httpapi.Options{
		DB: database, Auth: authSvc, MFA: &auth.MFA{DB: database, Key: mfaKey}, Mail: mailSvc,
		Devices: &devices.Service{DB: database}, Sync: syncSvc, Photos: photoSvc, Lister: listerSvc, Ebay: ebaySvc,
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
				if n, err := httpapi.PurgeClosed(ctx, authSvc, photoSvc); err != nil {
					log.Warn("purge closed accounts", "err", err)
				} else if n > 0 {
					log.Info("closed accounts deleted", "count", n)
				}
				if n, err := photoSvc.Cleanup(ctx); err != nil {
					log.Warn("photo cleanup", "err", err)
				} else if n > 0 {
					log.Info("old photos removed", "count", n)
				}
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
