package auth_test

import (
	"strings"
	"testing"

	"github.com/huda-salam/pamong/identity/adapter/auth"
)

func TestBcryptVerifier_RoundTrip(t *testing.T) {
	v := auth.NewBcryptVerifier()
	hash, err := v.Hash("rahasia-kuat")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash == "rahasia-kuat" || hash == "" {
		t.Fatalf("hash tidak boleh plaintext/kosong: %q", hash)
	}
	if err := v.Verify(hash, "rahasia-kuat"); err != nil {
		t.Fatalf("Verify password benar harus lulus: %v", err)
	}
	if err := v.Verify(hash, "salah"); err == nil {
		t.Fatal("Verify password salah harus gagal")
	}
}

func TestBcryptVerifier_TooLongRejected(t *testing.T) {
	v := auth.NewBcryptVerifier()
	if _, err := v.Hash(strings.Repeat("a", 73)); err == nil {
		t.Fatal("password >72 byte harus ditolak (cegah pemotongan diam-diam)")
	}
}

func TestBcryptVerifier_BrokenHash(t *testing.T) {
	v := auth.NewBcryptVerifier()
	if err := v.Verify("bukan-hash-bcrypt", "apa pun"); err == nil {
		t.Fatal("hash rusak harus gagal verify")
	}
}
