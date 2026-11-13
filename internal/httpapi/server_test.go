package httpapi

// Integration tests against a real Postgres database:
//
//	TEST_DATABASE_URL=postgres://user@/fcmweb_test?host=/tmp&port=55432&sslmode=disable go test ./...
//
// Without TEST_DATABASE_URL they are skipped. They wipe every table, so
// never point them at a real database.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/cloudsync"
	"github.com/jconner5017-rgb/fcm-web/internal/db"
	"github.com/jconner5017-rgb/fcm-web/internal/devices"
	"github.com/jconner5017-rgb/fcm-web/internal/email"
	"github.com/jconner5017-rgb/fcm-web/internal/license"
	"github.com/jconner5017-rgb/fcm-web/internal/photos"
)

type env struct {
	t      *testing.T
	db     *sql.DB
	srv    *httptest.Server
	now    time.Time
	bucket *fakeBucket
}

// fakeBucket stands in for R2: it stores whatever is PUT to a signed link.
// (Signatures are checked against AWS's own example in the photos package.)
type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (b *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch r.Method {
	case "PUT":
		if r.URL.Query().Get("X-Amz-Signature") == "" && r.Header.Get("Authorization") == "" {
			w.WriteHeader(403)
			return
		}
		data, _ := io.ReadAll(r.Body)
		b.objects[r.URL.Path] = data
	case "HEAD", "GET":
		data, ok := b.objects[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == "GET" {
			w.Write(data)
		}
	case "DELETE":
		delete(b.objects, r.URL.Path)
		w.WriteHeader(204)
	}
}

func setup(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	auth.PasswordIterations = 1000
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`TRUNCATE accounts, device_links, sync_changes, email_outbox, sync_photos CASCADE`); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: d, bucket: &fakeBucket{objects: map[string][]byte{}}}
	bucketSrv := httptest.NewServer(e.bucket)
	t.Cleanup(bucketSrv.Close)
	syncSvc := &cloudsync.Service{DB: d}
	photoSvc := &photos.Service{DB: d, Sync: syncSvc,
		Store: &photos.S3{Endpoint: bucketSrv.URL, Bucket: "fcm-photos", Region: "auto", AccessKey: "test", SecretKey: "test"}}
	authSvc := &auth.Service{DB: d, SessionTTL: time.Hour, AdminEmails: []string{"owner@example.com"}}
	key := make([]byte, 32)
	rand.Read(key)
	s := New(Options{
		DB: d, Auth: authSvc, MFA: &auth.MFA{DB: d, Key: key},
		Mail: &email.Service{DB: d}, Devices: &devices.Service{DB: d}, Sync: syncSvc, Photos: photoSvc,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), SessionTTL: time.Hour, WebURL: "https://app.test",
	})
	e.srv = httptest.NewServer(s.Handler())
	t.Cleanup(e.srv.Close)
	return e
}

// client is a browser (cookies) or a desktop app (bearer token).
type client struct {
	e     *env
	http  *http.Client
	token string
}

func (e *env) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.e.t.Helper()
	var rd io.Reader
	if body == nil && method == "POST" {
		body = map[string]any{} // like the website: every write is JSON
	}
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	h := c.http
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (c *client) must(want int, method, path string, body any) map[string]any {
	c.e.t.Helper()
	got, out := c.do(method, path, body)
	if got != want {
		c.e.t.Fatalf("%s %s: status %d, want %d: %v", method, path, got, want, out)
	}
	return out
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (e *env) signup(email string) *client {
	c := e.browser()
	c.must(201, "POST", "/v1/auth/signup", map[string]string{"email": email, "password": "long enough pw"})
	return c
}

// link runs the whole link flow for a desktop app and returns it.
func (e *env) link(owner *client, edition, hwid, key string) *client {
	e.t.Helper()
	app := &client{e: e}
	start := app.must(200, "POST", "/v1/devices/link/start", map[string]string{
		"device_name": "Shop PC", "edition": edition, "hwid": hwid, "license_key": key, "app_version": "1.0.14"})
	code := start["user_code"].(string)
	fin := map[string]string{"link_id": start["link_id"].(string), "poll_secret": start["poll_secret"].(string)}
	if out := app.must(202, "POST", "/v1/devices/link/finish", fin); out["status"] != "pending" {
		e.t.Fatalf("finish before approval: %v", out)
	}
	look := owner.must(200, "GET", "/v1/devices/link/"+strings.ToLower(strings.ReplaceAll(code, "-", "")), nil)
	if look["edition"] != edition || look["device_name"] != "Shop PC" {
		e.t.Fatalf("lookup: %v", look)
	}
	owner.must(200, "POST", "/v1/devices/link/approve", map[string]string{"user_code": code})
	done := app.must(200, "POST", "/v1/devices/link/finish", fin)
	app.token = done["device_token"].(string)
	if app.token == "" || done["device_id"] == "" {
		e.t.Fatalf("finish: %v", done)
	}
	if out := app.must(410, "POST", "/v1/devices/link/finish", fin); errCode(out) != "link_used" {
		e.t.Fatalf("second finish: %v", out)
	}
	return app
}

func TestAccountsAndTwoFactor(t *testing.T) {
	e := setup(t)
	c := e.signup("Seller@Example.com")
	me := c.must(200, "GET", "/v1/me", nil)
	if me["sync_allowed"] != true || me["mfa_enabled"] != false || me["card_limit"].(float64) != 10000 {
		t.Fatalf("me: %v", me)
	}

	if out := e.browser().must(409, "POST", "/v1/auth/signup", map[string]string{"email": "seller@example.com", "password": "long enough pw"}); errCode(out) != "email_taken" {
		t.Fatal(out)
	}
	out := e.browser().must(422, "POST", "/v1/auth/signup", map[string]string{"email": "nope", "password": "short"})
	if f := out["error"].(map[string]any)["fields"].(map[string]any); f["email"] == nil || f["password"] == nil {
		t.Fatal(out)
	}
	e.browser().must(401, "POST", "/v1/auth/login", map[string]string{"email": "seller@example.com", "password": "wrong password"})
	e.browser().must(401, "GET", "/v1/me", nil)

	// Turn on 2FA.
	setupOut := c.must(200, "POST", "/v1/me/mfa/setup", nil)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setupOut["secret"].(string))
	if err != nil || !strings.HasPrefix(setupOut["otpauth_url"].(string), "otpauth://totp/FireCardManager") {
		t.Fatalf("setup: %v %v", setupOut, err)
	}
	c.must(400, "POST", "/v1/me/mfa/confirm", map[string]string{"code": "000000"})
	codeNow := auth.TOTP(secret, time.Now().Unix()/30)
	conf := c.must(200, "POST", "/v1/me/mfa/confirm", map[string]string{"code": codeNow})
	backups := conf["backup_codes"].([]any)
	if len(backups) != 10 {
		t.Fatalf("backup codes: %v", conf)
	}

	// Signing in now needs the code; until then the session can do nothing else.
	b := e.browser()
	if out := b.must(200, "POST", "/v1/auth/login", map[string]string{"email": "SELLER@example.com", "password": "long enough pw"}); out["mfa_required"] != true {
		t.Fatalf("login: %v", out)
	}
	if out := b.must(401, "GET", "/v1/me", nil); errCode(out) != "mfa_required" {
		t.Fatal(out)
	}
	b.must(401, "POST", "/v1/auth/mfa", map[string]string{"code": "123456"})
	done := b.must(200, "POST", "/v1/auth/mfa", map[string]string{"code": backups[0].(string)})
	if done["used_backup"] != true || done["backups_left"].(float64) != 9 {
		t.Fatalf("mfa: %v", done)
	}
	b.must(200, "GET", "/v1/me", nil)

	// A backup code works once.
	b2 := e.browser()
	b2.must(200, "POST", "/v1/auth/login", map[string]string{"email": "seller@example.com", "password": "long enough pw"})
	b2.must(401, "POST", "/v1/auth/mfa", map[string]string{"code": backups[0].(string)})

	// Password change signs out other sessions.
	c.must(422, "POST", "/v1/me/password", map[string]string{"current_password": "nope", "password": "a new long password"})
	c.must(200, "POST", "/v1/me/password", map[string]string{"current_password": "long enough pw", "password": "a new long password"})
	b.must(401, "GET", "/v1/me", nil)
	c.must(200, "GET", "/v1/me", nil)

	c.must(204, "POST", "/v1/auth/logout", nil)
	c.must(401, "GET", "/v1/me", nil)
}

func TestPasswordResetAndVerify(t *testing.T) {
	e := setup(t)
	e.signup("reset@example.com")
	// Mail is off in tests: make the token the way the handler would.
	var id string
	e.db.QueryRow(`SELECT id FROM accounts WHERE email = 'reset@example.com'`).Scan(&id)
	mail := &email.Service{DB: e.db}
	tok, err := mail.NewToken(context.Background(), e.db, id, "reset@example.com", "reset", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b := e.browser()
	b.must(200, "POST", "/v1/auth/forgot-password", map[string]string{"email": "nobody@example.com"}) // same answer either way
	b.must(422, "POST", "/v1/auth/reset-password", map[string]string{"token": tok, "password": "short"})
	tok, _ = mail.NewToken(context.Background(), e.db, id, "reset@example.com", "reset", time.Hour)
	b.must(200, "POST", "/v1/auth/reset-password", map[string]string{"token": tok, "password": "brand new password"})
	b.must(400, "POST", "/v1/auth/reset-password", map[string]string{"token": tok, "password": "brand new password 2"})
	b.must(200, "POST", "/v1/auth/login", map[string]string{"email": "reset@example.com", "password": "brand new password"})
	if me := b.must(200, "GET", "/v1/me", nil); me["account"].(map[string]any)["email_verified"] != true {
		t.Fatalf("a reset link proves the email: %v", me)
	}
}

// signKey makes a license key with a test key pair, like the license server.
func signKey(t *testing.T, k *ecdsa.PrivateKey, edition, hwid string, expires time.Time) string {
	day := expires.Unix() / 86400
	msg := license.Prefix(edition) + "-License:" + hwid + ":" + strconv.FormatInt(day, 10)
	h := sha256.Sum256([]byte(msg))
	r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{byte(day >> 24), byte(day >> 16), byte(day >> 8), byte(day)}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(append(raw, sig...))
}

func TestLinkingAndLicenses(t *testing.T) {
	e := setup(t)
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	defer license.UseKeyForTest(&k.PublicKey)()

	owner := e.signup("owner2@example.com")
	hwid := "BD8C-3A99-A19C-D379"
	app := &client{e: e}

	// A key for another PC, the other edition, or past its grace is refused.
	good := signKey(t, k, "sports", hwid, time.Now().AddDate(1, 0, 0))
	if out := app.must(403, "POST", "/v1/devices/link/start", map[string]string{"edition": "sports", "hwid": "OTHER-PC", "license_key": good}); errCode(out) != "bad_license" {
		t.Fatal(out)
	}
	app.must(403, "POST", "/v1/devices/link/start", map[string]string{"edition": "tcg", "hwid": hwid, "license_key": good})
	old := signKey(t, k, "sports", hwid, time.Now().AddDate(0, 0, -30))
	if out := app.must(403, "POST", "/v1/devices/link/start", map[string]string{"edition": "sports", "hwid": hwid, "license_key": old}); errCode(out) != "license_expired" {
		t.Fatal(out)
	}
	app.must(400, "POST", "/v1/devices/link/start", map[string]string{"edition": "baseball", "hwid": hwid})

	sports := e.link(owner, "sports", hwid, good)
	tcg := e.link(owner, "tcg", hwid, "") // a desktop still on its trial
	var exp sql.NullTime
	e.db.QueryRow(`SELECT expires_at FROM licenses WHERE edition = 'sports'`).Scan(&exp)
	if !exp.Valid {
		t.Fatal("license expiry not recorded")
	}
	var leftover sql.NullString
	e.db.QueryRow(`SELECT license_key FROM device_links WHERE edition = 'sports'`).Scan(&leftover)
	if leftover.Valid {
		t.Fatal("the license key must not be kept after linking")
	}

	me := owner.must(200, "GET", "/v1/me", nil)
	if fmt.Sprint(me["editions"]) != "[sports tcg]" {
		t.Fatalf("editions: %v", me["editions"])
	}
	list := owner.must(200, "GET", "/v1/devices", nil)["devices"].([]any)
	if len(list) != 2 {
		t.Fatalf("devices: %v", list)
	}

	st := sports.must(200, "GET", "/v1/sync/status", nil)
	if st["edition"] != "sports" || st["account_email"] != "owner2@example.com" {
		t.Fatalf("status: %v", st)
	}

	// Re-linking the same app on the same PC replaces the old token.
	sports2 := e.link(owner, "sports", hwid, good)
	sports.must(401, "GET", "/v1/sync/status", nil)
	sports2.must(200, "GET", "/v1/sync/status", nil)

	// Unlinking from the website stops the token at once.
	var tcgID string
	for _, d := range owner.must(200, "GET", "/v1/devices", nil)["devices"].([]any) {
		if m := d.(map[string]any); m["edition"] == "tcg" {
			tcgID = m["id"].(string)
		}
	}
	other := e.signup("someone@example.com")
	other.must(404, "DELETE", "/v1/devices/"+tcgID, nil) // not theirs
	owner.must(204, "DELETE", "/v1/devices/"+tcgID, nil)
	if out := tcg.must(401, "GET", "/v1/sync/status", nil); errCode(out) != "device_unlinked" {
		t.Fatal(out)
	}

	// Codes expire, and an unknown code isn't found.
	owner.must(404, "GET", "/v1/devices/link/ZZZZ-ZZZZ", nil)
	owner.must(404, "POST", "/v1/devices/link/approve", map[string]string{"user_code": "ZZZZ-ZZZZ"})
}

// ---- sync ----

func uuid(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

func ch(entity, id string, base int, at time.Time, fields map[string]any) map[string]any {
	return map[string]any{"entity": entity, "id": id, "base_version": base, "edited_at": at.Format(time.RFC3339Nano), "fields": fields}
}

func push(t *testing.T, c *client, changes ...map[string]any) []map[string]any {
	t.Helper()
	out := c.must(200, "POST", "/v1/sync/push", map[string]any{"changes": changes})
	var res []map[string]any
	for _, r := range out["results"].([]any) {
		res = append(res, r.(map[string]any))
	}
	return res
}

func pull(t *testing.T, c *client, after int64) (map[string]map[string]any, int64) {
	t.Helper()
	out := c.must(200, "GET", fmt.Sprintf("/v1/sync/pull?after=%d", after), nil)
	rows := map[string]map[string]any{}
	for _, x := range out["changes"].([]any) {
		m := x.(map[string]any)
		rows[m["id"].(string)] = m
	}
	return rows, int64(out["next"].(float64))
}

func TestSync(t *testing.T) {
	e := setup(t)
	owner := e.signup("syncer@example.com")
	pcA := e.link(owner, "sports", "HWID-A", "")
	pcB := e.link(owner, "sports", "HWID-B", "")
	tcg := e.link(owner, "tcg", "HWID-A", "")
	t0 := time.Now().Add(-time.Hour)

	box, card1, card2 := uuid(1), uuid(2), uuid(3)
	res := push(t, pcA,
		ch("box", box, 0, t0, map[string]any{"name": "Box 1 - Basketball", "sport": "Basketball"}),
		ch("card", card1, 0, t0, map[string]any{"player_name": "Jared Bynum", "sale_price": 5.19, "status": "Ready", "box_id": box, "row": "A"}),
		ch("card", card2, 0, t0, map[string]any{"player_name": "Josh Giddey", "sale_price": 14.40, "status": "Ready", "box_id": box, "row": "A"}),
		ch("card", "not-a-uuid", 0, t0, map[string]any{"x": 1}),
		ch("card", uuid(9), 0, t0, map[string]any{"Bad Name": 1}),
	)
	for i, want := range []string{"applied", "applied", "applied", "rejected", "rejected"} {
		if res[i]["status"] != want {
			t.Fatalf("result %d: %v", i, res[i])
		}
	}
	var boxCol string
	e.db.QueryRow(`SELECT box_id::text FROM sync_cards WHERE id = $1`, card1).Scan(&boxCol)
	if boxCol != box {
		t.Fatalf("box_id column: %q", boxCol)
	}

	// The other PC sees all three; the TCG app sees none of them.
	rowsB, nextB := pull(t, pcB, 0)
	if len(rowsB) != 3 || rowsB[card1]["fields"].(map[string]any)["player_name"] != "Jared Bynum" {
		t.Fatalf("pull B: %v", rowsB)
	}
	if rowsT, _ := pull(t, tcg, 0); len(rowsT) != 0 {
		t.Fatalf("TCG app saw sports rows: %v", rowsT)
	}

	// Two PCs edit card1 between syncs. Different fields: both kept.
	push(t, pcB, ch("card", card1, 1, t0.Add(10*time.Minute), map[string]any{"sale_price": 6.00}))
	r := push(t, pcA, ch("card", card1, 1, t0.Add(5*time.Minute), map[string]any{"row": "B"}))[0]
	if r["status"] != "applied" || r["version"].(float64) != 3 {
		t.Fatalf("different fields: %v", r)
	}
	// Same field: the newer edit wins whichever arrives last.
	r = push(t, pcA, ch("card", card1, 1, t0.Add(2*time.Minute), map[string]any{"sale_price": 4.00}))[0]
	if r["status"] != "merged" || fmt.Sprint(r["lost_fields"]) != "[sale_price]" {
		t.Fatalf("older edit should lose: %v", r)
	}
	r = push(t, pcA, ch("card", card1, 1, t0.Add(20*time.Minute), map[string]any{"sale_price": 7.50}))[0]
	if r["status"] != "merged" || r["lost_fields"] != nil {
		t.Fatalf("newer edit should win: %v", r)
	}
	rowsB, nextB = pull(t, pcB, nextB)
	f := rowsB[card1]["fields"].(map[string]any)
	if f["sale_price"].(float64) != 7.50 || f["row"] != "B" || len(rowsB) != 1 {
		t.Fatalf("after merge: %v", rowsB)
	}
	var conflicts int
	e.db.QueryRow(`SELECT count(*) FROM sync_conflicts`).Scan(&conflicts)
	if conflicts != 2 {
		t.Fatalf("conflicts logged: %d", conflicts)
	}

	// Sold is final.
	push(t, pcB, ch("card", card2, 1, t0.Add(30*time.Minute), map[string]any{"status": "Sold"}))
	r = push(t, pcA, ch("card", card2, 2, t0.Add(40*time.Minute), map[string]any{"status": "Ready", "sale_price": 20}))[0]
	if fmt.Sprint(r["lost_fields"]) != "[status]" {
		t.Fatalf("sold should stay sold: %v", r)
	}
	var status string
	e.db.QueryRow(`SELECT status FROM sync_cards WHERE id = $1`, card2).Scan(&status)
	if status != "Sold" {
		t.Fatalf("status column: %s", status)
	}

	// Deletes win over later edits.
	r = push(t, pcB, map[string]any{"entity": "card", "id": card1, "base_version": 4, "deleted": true, "edited_at": t0.Add(50 * time.Minute)})[0]
	if r["status"] != "deleted" {
		t.Fatalf("delete: %v", r)
	}
	r = push(t, pcA, ch("card", card1, 4, t0.Add(55*time.Minute), map[string]any{"sale_price": 1}))[0]
	if r["status"] != "deleted" {
		t.Fatalf("edit after delete: %v", r)
	}
	rowsB, _ = pull(t, pcB, nextB)
	if rowsB[card1]["deleted"] != true {
		t.Fatalf("pull should carry the delete: %v", rowsB[card1])
	}

	// Another account can't touch or see these rows.
	stranger := e.link(e.signup("stranger@example.com"), "sports", "HWID-X", "")
	r = push(t, stranger, ch("card", card2, 5, time.Now(), map[string]any{"player_name": "Hijack"}))[0]
	if r["status"] != "rejected" || r["reason"] != "not found" {
		t.Fatalf("cross-account write: %v", r)
	}
	if rows, _ := pull(t, stranger, 0); len(rows) != 0 {
		t.Fatalf("cross-account read: %v", rows)
	}

	// A batch over 200 is refused whole.
	var many []map[string]any
	for i := 0; i < 201; i++ {
		many = append(many, ch("box", uuid(1000+i), 0, t0, map[string]any{"name": "x"}))
	}
	pcA.must(413, "POST", "/v1/sync/push", map[string]any{"changes": many})

	// Paging: limit splits the feed and `more` says there's more.
	out := pcB.must(200, "GET", "/v1/sync/pull?after=0&limit=2", nil)
	if out["more"] != true || len(out["changes"].([]any)) == 0 {
		t.Fatalf("paging: %v", out)
	}
}

func TestCardLimitAndTrial(t *testing.T) {
	e := setup(t)
	admin := e.signup("owner@example.com")
	seller := e.signup("limit@example.com")
	pc := e.link(seller, "sports", "HWID-L", "")
	var acct string
	e.db.QueryRow(`SELECT id FROM accounts WHERE email = 'limit@example.com'`).Scan(&acct)

	// Fill the account to the limit (a sold card doesn't count).
	if _, err := e.db.Exec(`INSERT INTO sync_cards (id, account_id, edition, status)
		SELECT gen_random_uuid(), $1, 'sports', 'Ready' FROM generate_series(1, $2)`, acct, cloudsync.CardLimit); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	res := push(t, pc,
		ch("card", uuid(1), 0, now, map[string]any{"status": "Ready"}),
		ch("card", uuid(2), 0, now, map[string]any{"status": "Sold"}),
		ch("box", uuid(3), 0, now, map[string]any{"name": "Box"}),
	)
	if res[0]["reason"] != "card_limit" || res[1]["status"] != "applied" || res[2]["status"] != "applied" {
		t.Fatalf("limit: %v", res)
	}

	// Trial over: push and pull stop, status still answers.
	e.db.Exec(`UPDATE accounts SET sync_trial_ends_at = now() - interval '1 day' WHERE id = $1`, acct)
	if out := pc.must(402, "POST", "/v1/sync/push", map[string]any{"changes": []any{}}); errCode(out) != "sync_inactive" {
		t.Fatal(out)
	}
	pc.must(402, "GET", "/v1/sync/pull", nil)
	if st := pc.must(200, "GET", "/v1/sync/status", nil); st["sync_allowed"] != false {
		t.Fatalf("status: %v", st)
	}

	// Admin tools need an admin with 2FA on.
	seller.must(403, "GET", "/v1/admin/accounts", nil)
	if out := admin.must(403, "GET", "/v1/admin/accounts", nil); errCode(out) != "mfa_setup_required" {
		t.Fatal(out)
	}
	sec := admin.must(200, "POST", "/v1/me/mfa/setup", nil)["secret"].(string)
	raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec)
	admin.must(200, "POST", "/v1/me/mfa/confirm", map[string]string{"code": auth.TOTP(raw, time.Now().Unix()/30)})
	list := admin.must(200, "GET", "/v1/admin/accounts?q=limit", nil)["accounts"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["cards"].(float64) != float64(cloudsync.CardLimit+1) {
		t.Fatalf("admin list: %v", list)
	}

	// The add-on (6 months for $20, 12 for $40) restores sync; a renewal adds on.
	admin.must(422, "POST", "/v1/admin/accounts/"+acct+"/addon", map[string]any{"months": 3})
	first := admin.must(200, "POST", "/v1/admin/accounts/"+acct+"/addon", map[string]any{"months": 6, "note": "Order 1001, $20"})
	pc.must(200, "GET", "/v1/sync/pull", nil)
	second := admin.must(200, "POST", "/v1/admin/accounts/"+acct+"/addon", map[string]any{"months": 12, "note": "Order 1002, $40"})
	t1, _ := time.Parse(time.RFC3339Nano, first["web_addon_until"].(string))
	t2, _ := time.Parse(time.RFC3339Nano, second["web_addon_until"].(string))
	if d := t2.Sub(t1); d < 360*24*time.Hour || d > 370*24*time.Hour {
		t.Fatalf("renewal should add 12 months to the current end: %v -> %v", t1, t2)
	}
	if st := pc.must(200, "GET", "/v1/sync/status", nil); st["web_addon"] != true {
		t.Fatalf("status: %v", st)
	}
	e.db.Exec(`UPDATE accounts SET web_addon_until = now() - interval '1 day' WHERE id = $1`, acct)
	pc.must(402, "GET", "/v1/sync/pull", nil) // the add-on ran out
	admin.must(200, "POST", "/v1/admin/accounts/"+acct+"/addon", map[string]any{"months": 6})
	pc.must(200, "GET", "/v1/sync/pull", nil)
	admin.must(200, "POST", "/v1/admin/accounts/"+acct+"/addon", map[string]any{"months": 0})
	pc.must(402, "GET", "/v1/sync/pull", nil)
	admin.must(200, "POST", "/v1/admin/accounts/"+acct+"/trial", map[string]any{"until": time.Now().AddDate(0, 0, 7).Format("2006-01-02")})
	pc.must(200, "GET", "/v1/sync/pull", nil)
	admin.must(422, "POST", "/v1/admin/accounts/"+acct+"/trial", map[string]any{"until": "next week"})
}

func TestWritesNeedJSON(t *testing.T) {
	e := setup(t)
	res, err := http.Post(e.srv.URL+"/v1/auth/login", "text/plain", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 415 {
		t.Fatalf("form-style post: %d", res.StatusCode)
	}
}

func TestPhotos(t *testing.T) {
	e := setup(t)
	owner := e.signup("photos@example.com")
	pcA := e.link(owner, "sports", "HWID-P1", "")
	pcB := e.link(owner, "sports", "HWID-P2", "")
	card := uuid(77)
	push(t, pcA, ch("card", card, 0, time.Now(), map[string]any{"player_name": "Nikola Topic", "status": "Ready"}))

	jpeg := bytes.Repeat([]byte{0xFF, 0xD8, 0xFF}, 1000)
	out := pcA.must(200, "POST", "/v1/photos/upload-urls", map[string]any{"photos": []map[string]any{
		{"card_id": card, "side": "front", "bytes": len(jpeg)},
		{"card_id": uuid(999), "side": "front", "bytes": 10}, // no such card
		{"card_id": card, "side": "sideways", "bytes": 10},   // bad side
		{"card_id": card, "side": "back", "bytes": 50 << 20}, // too big
	}})
	ups := out["uploads"].([]any)
	first := ups[0].(map[string]any)
	if first["upload_url"] == nil || first["error"] != nil {
		t.Fatalf("upload slot: %v", first)
	}
	for i := 1; i < 4; i++ {
		if ups[i].(map[string]any)["error"] == nil {
			t.Fatalf("slot %d should be refused: %v", i, ups[i])
		}
	}
	photoID := first["photo_id"].(string)

	// Confirming before the upload lands says missing; the card is untouched.
	res := pcA.must(200, "POST", "/v1/photos/confirm", map[string]any{"photo_ids": []string{photoID}})
	if res["photos"].([]any)[0].(map[string]any)["status"] != "missing" {
		t.Fatalf("confirm before upload: %v", res)
	}
	req, _ := http.NewRequest("PUT", first["upload_url"].(string), bytes.NewReader(jpeg))
	req.Header.Set("Content-Type", "image/jpeg")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 200 {
		t.Fatalf("upload to bucket: %v %v", r, err)
	}
	res = pcA.must(200, "POST", "/v1/photos/confirm", map[string]any{"photo_ids": []string{photoID}})
	if c := res["photos"].([]any)[0].(map[string]any); c["status"] != "ready" || c["card_version"].(float64) != 2 {
		t.Fatalf("confirm: %v", res)
	}

	// The other PC pulls the card with its new photo id and can fetch it.
	rows, _ := pull(t, pcB, 0)
	if rows[card]["fields"].(map[string]any)["front_photo_id"] != photoID {
		t.Fatalf("pull: %v", rows[card])
	}
	view := pcB.must(200, "GET", "/v1/photos/"+photoID+"?format=json", nil)
	got, err := http.Get(view["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(body, jpeg) {
		t.Fatalf("downloaded %d bytes", len(body))
	}

	// The website gets a redirect to the same short-lived link.
	noFollow := &http.Client{Jar: owner.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	rr, _ := noFollow.Get(e.srv.URL + "/v1/photos/" + photoID)
	if rr.StatusCode != 302 || !strings.Contains(rr.Header.Get("Location"), "X-Amz-Signature=") {
		t.Fatalf("website view: %d %s", rr.StatusCode, rr.Header.Get("Location"))
	}

	// The permanent link for Shopify/Whatnot works only with the add-on (or for admins).
	pub, _ := http.Get(e.srv.URL + "/v1/p/" + photoID + ".jpg")
	if pub.StatusCode != 403 {
		t.Fatalf("public link without add-on: %d", pub.StatusCode)
	}
	pub.Body.Close()
	if st := pcA.must(200, "GET", "/v1/sync/status", nil); st["photo_links"] != false {
		t.Fatalf("status photo_links without add-on: %v", st["photo_links"])
	}
	e.db.Exec(`UPDATE accounts SET web_addon_until = now() + interval '6 months' WHERE email = 'photos@example.com'`)
	pub, _ = http.Get(e.srv.URL + "/v1/p/" + strings.ToUpper(photoID) + ".jpg")
	pubBody, _ := io.ReadAll(pub.Body)
	pub.Body.Close()
	if pub.StatusCode != 200 || pub.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(pubBody, jpeg) {
		t.Fatalf("public link with add-on: %d %s %d bytes", pub.StatusCode, pub.Header.Get("Content-Type"), len(pubBody))
	}
	if st := pcA.must(200, "GET", "/v1/sync/status", nil); st["photo_links"] != true {
		t.Fatalf("status photo_links with add-on: %v", st["photo_links"])
	}
	for _, bad := range []string{photoID, "not-a-uuid.jpg", uuid(5) + ".jpg"} {
		if r, _ := http.Get(e.srv.URL + "/v1/p/" + bad); r.StatusCode != 404 {
			t.Fatalf("%s: %d", bad, r.StatusCode)
		}
	}

	// The inventory list carries a direct, signed link for each tile; it
	// stays the same within the hour so the browser can reuse its copy.
	tile := func() string {
		return owner.must(200, "GET", "/v1/inventory/cards", nil)["cards"].([]any)[0].(map[string]any)["thumb_url"].(string)
	}
	u1 := tile()
	if u1 != tile() || !strings.Contains(u1, "response-cache-control=") {
		t.Fatalf("tile link: %s", u1)
	}
	got, err = http.Get(u1)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(body, jpeg) {
		t.Fatalf("tile download: %d bytes", len(body))
	}
	if c := owner.must(200, "GET", "/v1/inventory/cards/"+card, nil); c["front_url"] == nil || c["back_url"] != nil {
		t.Fatalf("card photo links: %v", c)
	}

	// Someone else's account can't see it.
	stranger := e.link(e.signup("nosy@example.com"), "sports", "HWID-N", "")
	stranger.must(404, "GET", "/v1/photos/"+photoID, nil)

	// A new front replaces the old one; cleanup removes the old object.
	out = pcA.must(200, "POST", "/v1/photos/upload-urls", map[string]any{"photos": []map[string]any{{"card_id": card, "side": "front", "bytes": 5}}})
	slot := out["uploads"].([]any)[0].(map[string]any)
	req, _ = http.NewRequest("PUT", slot["upload_url"].(string), bytes.NewReader([]byte("12345")))
	http.DefaultClient.Do(req)
	pcA.must(200, "POST", "/v1/photos/confirm", map[string]any{"photo_ids": []string{slot["photo_id"].(string)}})
	if len(e.bucket.objects) != 2 {
		t.Fatalf("objects before cleanup: %d", len(e.bucket.objects))
	}
	ps := &photos.Service{DB: e.db, Store: &photos.S3{Endpoint: strings.Split(first["upload_url"].(string), "/fcm-photos/")[0], Bucket: "fcm-photos", Region: "auto", AccessKey: "test", SecretKey: "test"}}
	if n, err := ps.Cleanup(context.Background()); err != nil || n != 1 {
		t.Fatalf("cleanup: %d %v", n, err)
	}
	if len(e.bucket.objects) != 1 {
		t.Fatalf("objects after cleanup: %d", len(e.bucket.objects))
	}
	pcA.must(404, "GET", "/v1/photos/"+photoID, nil) // the replaced photo is gone
}

func TestInventory(t *testing.T) {
	e := setup(t)
	owner := e.signup("inv@example.com")
	pc := e.link(owner, "sports", "HWID-I1", "")
	tcg := e.link(owner, "tcg", "HWID-I2", "")
	now := time.Now()
	boxA, boxB := uuid(501), uuid(502)
	push(t, pc,
		ch("box", boxA, 0, now, map[string]any{"name": "Basketball 1"}),
		ch("box", boxB, 0, now, map[string]any{"name": "Baseball 1"}),
		ch("card", uuid(601), 0, now, map[string]any{"player_name": "Cole Anthony", "set_name": "Prizm", "year": 2023,
			"estimated_value": 3.12, "status": "Ready", "box_id": boxA, "slot_row": "A", "slot_number": 2, "sku": "FCM-1471"}),
		ch("card", uuid(602), 0, now, map[string]any{"player_name": "Max Strus", "set_name": "Hoops", "year": 2024,
			"estimated_value": 5, "status": "EBF", "box_id": boxA, "slot_row": "A", "slot_number": 1}),
		ch("card", uuid(603), 0, now, map[string]any{"player_name": "OG Anunoby", "set_name": "Donruss 100%_x", "year": 2021,
			"estimated_value": 15, "status": "Ready", "box_id": boxA, "slot_row": "B", "slot_number": 1}),
		ch("card", uuid(604), 0, now, map[string]any{"player_name": "Shohei Ohtani", "year": 2018, "estimated_value": 40,
			"status": "NotForSale", "box_id": boxB, "slot_row": "A", "slot_number": 1}),
		ch("card", uuid(605), 0, now, map[string]any{"player_name": "Loose Card", "status": "Ready"}),
		ch("card", uuid(606), 0, now, map[string]any{"player_name": "Gone Card", "status": "Sold", "box_id": boxA}),
		ch("sold", uuid(701), 0, now, map[string]any{"player_name": "Gone Card", "sale_price": 12.5, "sale_date": "2026-11-01T00:00:00"}),
		ch("sold", uuid(702), 0, now, map[string]any{"player_name": "Other Sale", "sale_price": 2, "sale_date": "2026-11-05T00:00:00"}),
	)
	push(t, tcg, ch("card", uuid(801), 0, now, map[string]any{"player_name": "Charizard", "status": "Ready"}))

	sum := owner.must(200, "GET", "/v1/inventory/summary", nil)
	if sum["edition"] != "sports" || sum["cards"].(float64) != 5 || sum["unboxed"].(float64) != 1 || sum["sold"].(float64) != 2 {
		t.Fatalf("summary: %v", sum)
	}
	if v := sum["value"].(float64); v < 63.11 || v > 63.13 {
		t.Fatalf("value %v", v)
	}
	boxes := sum["boxes"].([]any)
	first := boxes[0].(map[string]any) // sorted by name: Baseball 1 first
	second := boxes[1].(map[string]any)
	if first["name"] != "Baseball 1" || second["cards"].(float64) != 3 || len(second["rows"].([]any)) != 2 {
		t.Fatalf("boxes: %v", boxes)
	}
	if len(sum["editions"].([]any)) != 2 {
		t.Fatalf("editions: %v", sum["editions"])
	}
	if s := owner.must(200, "GET", "/v1/inventory/summary?edition=tcg", nil); s["cards"].(float64) != 1 {
		t.Fatalf("tcg summary: %v", s)
	}

	names := func(path string) []string {
		out := owner.must(200, "GET", path, nil)
		var n []string
		for _, c := range out["cards"].([]any) {
			n = append(n, c.(map[string]any)["data"].(map[string]any)["player_name"].(string))
		}
		return n
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// Default order: by box, row, slot; loose cards last.
	eq(names("/v1/inventory/cards"), "Shohei Ohtani", "Max Strus", "Cole Anthony", "OG Anunoby", "Loose Card")
	eq(names("/v1/inventory/cards?box="+boxA+"&row=A"), "Max Strus", "Cole Anthony")
	eq(names("/v1/inventory/cards?box=none"), "Loose Card")
	eq(names("/v1/inventory/cards?sort=value&dir=desc"), "Shohei Ohtani", "OG Anunoby", "Max Strus", "Cole Anthony", "Loose Card")
	eq(names("/v1/inventory/cards?status=listed"), "Max Strus")
	eq(names("/v1/inventory/cards?q=fcm-1471"), "Cole Anthony")
	eq(names("/v1/inventory/cards?q=basketball+prizm"), "Cole Anthony") // box name + set
	eq(names("/v1/inventory/cards?q=100%25_"), "OG Anunoby")            // % and _ are literal
	eq(names("/v1/inventory/cards?q=nobody"))
	eq(names("/v1/inventory/cards?edition=tcg"), "Charizard")
	pg := owner.must(200, "GET", "/v1/inventory/cards?per=2&page=3", nil)
	if pg["total"].(float64) != 5 || len(pg["cards"].([]any)) != 1 {
		t.Fatalf("paging: %v", pg)
	}
	if pg = owner.must(200, "GET", "/v1/inventory/cards?per=2&page=9", nil); pg["total"].(float64) != 5 {
		t.Fatalf("past the end: %v", pg)
	}

	card := owner.must(200, "GET", "/v1/inventory/cards/"+uuid(601), nil)
	if card["box_name"] != "Basketball 1" || card["data"].(map[string]any)["sku"] != "FCM-1471" {
		t.Fatalf("card: %v", card)
	}
	sold := owner.must(200, "GET", "/v1/inventory/sold", nil)
	if sold["total"].(float64) != 2 || sold["revenue"].(float64) != 14.5 ||
		sold["sold"].([]any)[0].(map[string]any)["data"].(map[string]any)["player_name"] != "Other Sale" {
		t.Fatalf("sold: %v", sold)
	}

	// Another account sees none of it.
	other := e.signup("inv2@example.com")
	if code, _ := other.do("GET", "/v1/inventory/cards/"+uuid(601), nil); code != 404 {
		t.Fatalf("other account card: %d", code)
	}
	if s := other.must(200, "GET", "/v1/inventory/summary", nil); s["cards"].(float64) != 0 {
		t.Fatalf("other summary: %v", s)
	}
	if code, _ := e.browser().do("GET", "/v1/inventory/summary", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
}

func TestInventoryEdits(t *testing.T) {
	e := setup(t)
	owner := e.signup("edit@example.com")
	pc := e.link(owner, "sports", "HWID-E1", "")
	now := time.Now()
	boxA, boxB := uuid(511), uuid(512)
	card, other, live := uuid(611), uuid(612), uuid(613)
	push(t, pc,
		ch("box", boxA, 0, now, map[string]any{"name": "Box A"}),
		ch("box", boxB, 0, now, map[string]any{"name": "Box B"}),
		ch("card", card, 0, now, map[string]any{"player_name": "Cole Anthony", "brand": "Panini", "set_name": "Prizm", "card_number": "12",
			"condition": "Raw - NM", "year": 2023, "estimated_value": 3.12, "status": "Ready", "box_id": boxA, "slot_row": "A", "slot_number": 1,
			"front_photo_id": uuid(901)}),
		ch("card", other, 0, now, map[string]any{"player_name": "Max Strus", "status": "Ready", "box_id": boxB, "slot_row": "C", "slot_number": 4}),
		ch("card", live, 0, now, map[string]any{"player_name": "Live One", "status": "EBF", "box_id": boxB, "slot_row": "C", "slot_number": 5}),
	)
	_, seq := pull(t, pc, 0)

	// A price and status edit goes through and reaches the PC.
	out := owner.must(200, "PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 1,
		"fields": map[string]any{"sale_price": 4.999, "status": "NotForSale", "variety": "  Silver  "}})
	d := out["card"].(map[string]any)["data"].(map[string]any)
	if d["sale_price"].(float64) != 5 || d["status"] != "NotForSale" || d["variety"] != "Silver" || d["updated_at"] == nil {
		t.Fatalf("edit: %v", d)
	}
	rows, _ := pull(t, pc, seq)
	if f := rows[card]["fields"].(map[string]any); f["sale_price"].(float64) != 5 || rows[card]["from_device"] != nil && rows[card]["from_device"] != "" {
		t.Fatalf("pulled: %v", rows[card])
	}

	// Bad values and fields are refused with reasons.
	code, res := owner.do("PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 2,
		"fields": map[string]any{"player_name": " ", "year": 20, "status": "Sold", "sku": "X", "slot_number": 1.5}})
	f := res["error"].(map[string]any)["fields"].(map[string]any)
	if code != 422 || len(f) != 5 {
		t.Fatalf("validation: %d %v", code, res)
	}
	// Status can't change while live on eBay.
	if code, _ := owner.do("PATCH", "/v1/inventory/cards/"+live, map[string]any{"base_version": 1, "fields": map[string]any{"status": "Ready"}}); code != 422 {
		t.Fatalf("live status: %d", code)
	}

	// Moving into a taken slot is refused; a free one works.
	code, res = owner.do("PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 2,
		"fields": map[string]any{"box_id": boxB, "slot_row": "c", "slot_number": 4}})
	if code != 422 || !strings.Contains(res["error"].(map[string]any)["fields"].(map[string]any)["slot_number"].(string), "Max Strus") {
		t.Fatalf("taken slot: %d %v", code, res)
	}
	out = owner.must(200, "PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 2,
		"fields": map[string]any{"box_id": boxB, "slot_row": "c", "slot_number": 6}})
	if c := out["card"].(map[string]any); c["box_name"] != "Box B" || c["data"].(map[string]any)["slot_row"] != "C" {
		t.Fatalf("move: %v", c)
	}
	if code, _ := owner.do("PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 3,
		"fields": map[string]any{"box_id": uuid(999)}}); code != 422 {
		t.Fatalf("unknown box: %d", code)
	}

	// A PC edit the website didn't see becomes a conflict when the website overwrites it.
	push(t, pc, ch("card", card, 3, time.Now(), map[string]any{"estimated_value": 9}))
	owner.must(200, "PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": 3, "fields": map[string]any{"estimated_value": 7}})
	cs := owner.must(200, "GET", "/v1/inventory/conflicts", nil)["conflicts"].([]any)
	if len(cs) != 1 {
		t.Fatalf("conflicts: %v", cs)
	}
	k := cs[0].(map[string]any)
	if k["field"] != "estimated_value" || k["kept"].(float64) != 7 || k["lost"].(float64) != 9 || k["name"] != "Cole Anthony" {
		t.Fatalf("conflict: %v", k)
	}
	owner.must(204, "POST", fmt.Sprintf("/v1/inventory/conflicts/%d", int(k["id"].(float64))), map[string]any{"use": "lost"})
	if v := owner.must(200, "GET", "/v1/inventory/cards/"+card, nil)["data"].(map[string]any)["estimated_value"].(float64); v != 9 {
		t.Fatalf("after resolve: %v", v)
	}
	if cs := owner.must(200, "GET", "/v1/inventory/conflicts", nil)["conflicts"].([]any); len(cs) != 0 {
		t.Fatalf("still open: %v", cs)
	}

	// Mark sold: a Sold Log entry with the card's details, the card Sold, inventory down by one.
	v := int(owner.must(200, "GET", "/v1/inventory/cards/"+card, nil)["version"].(float64))
	sold := owner.must(200, "POST", "/v1/inventory/cards/"+card+"/sold", map[string]any{"base_version": v, "sale_price": 20,
		"shipping_charged": 0, "where": "in_person", "buyer": "Show buyer", "sale_date": time.Now().Format(time.RFC3339)})
	soldID := sold["sold_id"].(string)
	list := owner.must(200, "GET", "/v1/inventory/sold", nil)
	sd := list["sold"].([]any)[0].(map[string]any)
	sdd := sd["data"].(map[string]any)
	if sd["id"] != soldID || sdd["player_name"] != "Cole Anthony" || sdd["sale_price"].(float64) != 20 || sdd["sold_outside_ebay"] != true ||
		sdd["estimated_value_at_sale"].(float64) != 9 || sdd["card_id"] != card || sdd["front_photo_id"] != uuid(901) {
		t.Fatalf("sold entry: %v", sdd)
	}
	cd := owner.must(200, "GET", "/v1/inventory/cards/"+card, nil)["data"].(map[string]any)
	if cd["status"] != "Sold" || cd["sold_entry_id"] != soldID {
		t.Fatalf("card after sale: %v", cd)
	}
	if s := owner.must(200, "GET", "/v1/inventory/summary", nil); s["cards"].(float64) != 2 || s["sold"].(float64) != 1 {
		t.Fatalf("summary after sale: %v", s)
	}
	if code, _ := owner.do("POST", "/v1/inventory/cards/"+card+"/sold", map[string]any{"base_version": v + 1, "sale_price": 1, "where": "ebay"}); code != 409 {
		t.Fatalf("sold twice: %d", code)
	}
	if code, _ := owner.do("PATCH", "/v1/inventory/cards/"+card, map[string]any{"base_version": v + 1, "fields": map[string]any{"sale_price": 1}}); code != 409 {
		t.Fatalf("edit sold: %d", code)
	}
	rows, _ = pull(t, pc, seq)
	if rows[soldID] == nil || rows[card]["fields"].(map[string]any)["status"] != "Sold" {
		t.Fatalf("PC pull after sale: %v", rows)
	}

	// Other accounts can't touch it; without the trial or add-on, edits stop.
	stranger := e.signup("edit2@example.com")
	if code, _ := stranger.do("PATCH", "/v1/inventory/cards/"+other, map[string]any{"base_version": 1, "fields": map[string]any{"sale_price": 1}}); code != 404 {
		t.Fatalf("stranger edit: %d", code)
	}
	if _, err := e.db.Exec(`UPDATE accounts SET sync_trial_ends_at = now() - interval '1 day' WHERE email = 'edit@example.com'`); err != nil {
		t.Fatal(err)
	}
	if code, _ := owner.do("PATCH", "/v1/inventory/cards/"+other, map[string]any{"base_version": 1, "fields": map[string]any{"sale_price": 1}}); code != 402 {
		t.Fatalf("no add-on: %d", code)
	}
}
