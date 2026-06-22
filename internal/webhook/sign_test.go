package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	s := GenerateSecret()
	if !strings.HasPrefix(s, "whsec_") || len(s) < 20 {
		t.Fatalf("secret inválido: %q", s)
	}
	if GenerateSecret() == s {
		t.Fatal("dos secrets deben diferir")
	}
}

func TestSignatureMatchesBackendScheme(t *testing.T) {
	secret := "whsec_test"
	ts := int64(1780700000)
	body := []byte(`{"id":"x"}`)
	got := Signature(secret, ts, body)
	if !strings.HasPrefix(got, "t=1780700000,v1=") {
		t.Fatalf("formato: %q", got)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("1780700000." + string(body)))
	want := "t=1780700000,v1=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("firma:\n got %q\nwant %q", got, want)
	}
}
