package license

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sign makes a key the way the license server does, with a test key pair.
func sign(t *testing.T, k *ecdsa.PrivateKey, msg string, prefix []byte) string {
	t.Helper()
	h := sha256.Sum256([]byte(msg))
	r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	raw := append(append([]byte{}, prefix...), sig...)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	var parts []string
	for len(enc) > 5 {
		parts = append(parts, enc[:5])
		enc = enc[5:]
	}
	return strings.Join(append(parts, enc), "-")
}

func TestCheck(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	saved := pub
	pub = &k.PublicKey
	defer func() { pub = saved }()

	hwid := "AAAA-BBBB-CCCC-DDDD"
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	legacy := sign(t, k, "FireCardManager-License:"+hwid, nil)
	if r := Check("sports", hwid, legacy, now); r.State != NoExpiry {
		t.Fatalf("legacy sports key: %v", r.State)
	}
	if r := Check("tcg", hwid, legacy, now); r.State != Invalid {
		t.Fatalf("a sports key must not unlock TCG: %v", r.State)
	}
	if r := Check("sports", "AAAA-BBBB-CCCC-EEEE", legacy, now); r.State != Invalid {
		t.Fatalf("another PC's hardware ID must fail: %v", r.State)
	}
	if r := Check("sports", hwid, strings.ToLower(strings.ReplaceAll(legacy, "-", " ")), now); r.State != NoExpiry {
		t.Fatalf("case and separators should not matter: %v", r.State)
	}

	day := now.Unix()/86400 + 100
	prefix := []byte{byte(day >> 24), byte(day >> 16), byte(day >> 8), byte(day)}
	tcg := sign(t, k, "FireCardManagerTCG-License:"+hwid+":"+strconv.FormatInt(day, 10), prefix)
	r := Check("tcg", hwid, tcg, now)
	if r.State != Valid || r.ExpiresAt == nil || r.ExpiresAt.Unix() != day*86400 {
		t.Fatalf("new TCG key: %+v", r)
	}
	if r := Check("tcg", hwid, tcg, now.AddDate(0, 0, 105)); r.State != Grace {
		t.Fatalf("5 days past expiry should be grace: %v", r.State)
	}
	if r := Check("tcg", hwid, tcg, now.AddDate(0, 0, 120)); r.State != Expired {
		t.Fatalf("20 days past expiry should be expired: %v", r.State)
	}

	// Changing the expiry bytes breaks the signature.
	forged := sign(t, k, "FireCardManagerTCG-License:"+hwid+":"+strconv.FormatInt(day, 10), []byte{0, 0, 0xFF, 0xFF})
	if r := Check("tcg", hwid, forged, now); r.State != Invalid {
		t.Fatalf("edited expiry must fail: %v", r.State)
	}
	for _, bad := range []string{"", "not-a-key!", "AAAAA-BBBBB"} {
		if r := Check("sports", hwid, bad, now); r.State != Invalid {
			t.Fatalf("%q: %v", bad, r.State)
		}
	}
}

func TestRealPublicKeyParses(t *testing.T) {
	if pub == nil || pub.Curve != elliptic.P256() {
		t.Fatal("public key should be P-256")
	}
}
