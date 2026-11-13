// Package license checks FireCardManager license keys the same way the
// desktop apps do (Services/Licensing/LicenseService.cs in both apps), so
// the website can confirm which editions a seller owns without calling the
// license server. Keys are issued per hardware ID on the license server:
//
//   - legacy key: base32 of a 64-byte P-256 signature over
//     "<Prefix>-License:<hwid>"; never expires.
//   - new key: base32 of a 4-byte big-endian expiry (whole days since the
//     Unix epoch) followed by a 64-byte signature over
//     "<Prefix>-License:<hwid>:<expiry day>".
//
// Prefix is "FireCardManager" for the Sports app and "FireCardManagerTCG"
// for the TCG app. Both apps use the same public key.
package license

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// PublicKeyBase64 is the apps' public key (SubjectPublicKeyInfo DER). Only
// the public half: whoever holds the private key on the license server can
// issue keys; this can only check them.
const PublicKeyBase64 = "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAETFuzuAaMksn/af/SgIj/gcMf2HDfW2z0xLFmikUrBg6u1ZmExDXBdtPduKdFbgm7rNJ6xYeGNHW5PTZ4vCNXtg=="

// GraceDays matches the apps: a key keeps working this long past expiry.
const GraceDays = 14

// State of a key, matching the apps' LicenseState.
type State string

const (
	Invalid  State = "invalid"
	NoExpiry State = "no_expiry"
	Valid    State = "valid"
	Grace    State = "grace"
	Expired  State = "expired"
)

// Usable reports whether the app itself would keep running with this key.
func (s State) Usable() bool { return s == NoExpiry || s == Valid || s == Grace }

// Result of checking a key.
type Result struct {
	State     State
	ExpiresAt *time.Time // nil for legacy keys
}

var pub = func() *ecdsa.PublicKey {
	der, err := base64.StdEncoding.DecodeString(PublicKeyBase64)
	if err != nil {
		panic(err)
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		panic(err)
	}
	return k.(*ecdsa.PublicKey)
}()

// Prefix returns the signing prefix for an edition ("sports" or "tcg").
func Prefix(edition string) string {
	if edition == "tcg" {
		return "FireCardManagerTCG"
	}
	return "FireCardManager"
}

// Check verifies key for hwid and edition at time now.
func Check(edition, hwid, key string, now time.Time) Result {
	decoded, ok := base32Decode(key)
	if !ok {
		return Result{State: Invalid}
	}
	prefix := Prefix(edition)
	switch len(decoded) {
	case 64:
		if verify([]byte(prefix+"-License:"+hwid), decoded) {
			return Result{State: NoExpiry}
		}
		return Result{State: Invalid}
	case 68:
		day := int64(decoded[0])<<24 | int64(decoded[1])<<16 | int64(decoded[2])<<8 | int64(decoded[3])
		if !verify([]byte(prefix+"-License:"+hwid+":"+strconv.FormatInt(day, 10)), decoded[4:]) {
			return Result{State: Invalid}
		}
		exp := time.Unix(day*86400, 0).UTC()
		switch {
		case !now.After(exp):
			return Result{State: Valid, ExpiresAt: &exp}
		case !now.After(exp.AddDate(0, 0, GraceDays)):
			return Result{State: Grace, ExpiresAt: &exp}
		default:
			return Result{State: Expired, ExpiresAt: &exp}
		}
	}
	return Result{State: Invalid}
}

// verify checks an IEEE P1363 (r||s) P-256 signature over SHA-256(msg).
func verify(msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	h := sha256.Sum256(msg)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(pub, h[:], r, s)
}

// base32Decode is RFC 4648 base32 without padding, case-insensitive, with
// dashes and whitespace ignored, exactly like the apps' Base32Decode.
func base32Decode(input string) ([]byte, bool) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '-', ' ', '\r', '\n', '\t':
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(input)))
	if cleaned == "" {
		return nil, false
	}
	out := make([]byte, 0, len(cleaned)*5/8)
	buf, bits := 0, 0
	for _, c := range cleaned {
		v := strings.IndexRune(alphabet, c)
		if v < 0 {
			return nil, false
		}
		buf = (buf<<5 | v) & 0xFFFF
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buf>>bits))
		}
	}
	return out, true
}

// UseKeyForTest swaps the public key (tests in other packages sign their own
// keys) and returns a func that puts the real one back.
func UseKeyForTest(k *ecdsa.PublicKey) (restore func()) {
	saved := pub
	pub = k
	return func() { pub = saved }
}
