package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	PasswordIterations = 1000
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$1000$") {
		t.Fatalf("unexpected format: %s", h)
	}
	if !VerifyPassword("correct horse battery", h) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong password!!", h) {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("same password produced identical hashes; salt not random")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "plain", "bcrypt$1$a$b", "pbkdf2-sha256$x$a$b", "pbkdf2-sha256$10$!!$!!"} {
		if VerifyPassword("anything", bad) {
			t.Fatalf("accepted malformed hash %q", bad)
		}
	}
}

func TestValidateSignup(t *testing.T) {
	cases := []struct {
		email, user, pass string
		badFields         []string
	}{
		{"a@b.co", "card_guy", "longenough1", nil},
		{"not-an-email", "card_guy", "longenough1", []string{"email"}},
		{"Name <a@b.co>", "card_guy", "longenough1", []string{"email"}},
		{"a@b.co", "card_guy", "short", []string{"password"}},
	}
	for _, c := range cases {
		err := validateSignup(c.email, c.pass)
		if c.badFields == nil {
			if err != nil {
				t.Errorf("%+v: unexpected error %v", c, err)
			}
			continue
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Errorf("%+v: want ValidationError, got %v", c, err)
			continue
		}
		for _, f := range c.badFields {
			if _, ok := ve.Fields[f]; !ok {
				t.Errorf("%+v: expected error on %s, got %v", c, f, ve.Fields)
			}
		}
	}
}

func TestParseKey(t *testing.T) {
	if k, err := ParseKey(strings.Repeat("ab", 32)); err != nil || len(k) != 32 {
		t.Fatalf("hex: %v %d", err, len(k))
	}
	if k, err := ParseKey("rNd0m-Render-Generated-Value-Of-Some-Length-1234567890"); err != nil || len(k) != 32 {
		t.Fatalf("long random string: %v %d", err, len(k))
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("a short key must be refused")
	}
	if k, err := ParseKey(""); err != nil || k != nil {
		t.Fatal("empty means off")
	}
}
