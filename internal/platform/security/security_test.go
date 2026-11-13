package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestPasswordHashVerify(t *testing.T) {
	h := NewPasswordHasher(Argon2Params{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32})
	enc, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("unexpected PHC format: %s", enc)
	}
	ok, rehash, err := h.Verify("correct horse battery", enc)
	if err != nil || !ok || rehash {
		t.Fatalf("verify = %v %v %v", ok, rehash, err)
	}
	if ok, _, _ := h.Verify("wrong", enc); ok {
		t.Fatal("wrong password accepted")
	}
	stronger := NewPasswordHasher(Argon2Params{MemoryKiB: 16 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32})
	if _, rehash, _ := stronger.Verify("correct horse battery", enc); !rehash {
		t.Fatal("expected rehash when params change")
	}
	if _, _, err := h.Verify("x", "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"); err == nil {
		t.Fatal("legacy sha256 hash must be rejected")
	}
}

func TestJWTSignVerify(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := NewJWTSigner(priv, "pfm", "pfm-api", 15*time.Minute)
	uid := uuid.New()
	tok, _, err := s.Sign(uid, "ADMIN", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Verify(tok)
	if err != nil || c.Subject != uid.String() || c.Role != "ADMIN" {
		t.Fatalf("verify: %v %+v", err, c)
	}

	// Expired token.
	s.now = func() time.Time { return time.Now().Add(-time.Hour) }
	old, _, _ := s.Sign(uid, "ADMIN", nil)
	s.now = time.Now
	if _, err := s.Verify(old); err == nil {
		t.Fatal("expired token accepted")
	}

	// Wrong audience.
	other := NewJWTSigner(priv, "pfm", "other", 15*time.Minute)
	tokOther, _, _ := other.Sign(uid, "ADMIN", nil)
	if _, err := s.Verify(tokOther); err == nil {
		t.Fatal("token for another audience accepted")
	}

	// Algorithm confusion: HS256 signed with the public key must be rejected.
	pub := priv.Public().(ed25519.PublicKey)
	hs, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: uid.String(),
		Issuer: "pfm", Audience: jwt.ClaimStrings{"pfm-api"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now())}).SignedString([]byte(pub))
	if _, err := s.Verify(hs); err == nil {
		t.Fatal("HS256 token accepted")
	}

	// Different key.
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)
	forged, _, _ := NewJWTSigner(priv2, "pfm", "pfm-api", time.Minute).Sign(uid, "ADMIN", nil)
	if _, err := s.Verify(forged); err == nil {
		t.Fatal("token signed by another key accepted")
	}
}

func TestOpaqueTokenAndMask(t *testing.T) {
	raw, hash, err := NewOpaqueToken()
	if err != nil || len(raw) != 43 || len(hash) != 64 || HashToken(raw) != hash {
		t.Fatalf("token: %q %q %v", raw, hash, err)
	}
	if got := MaskMiddle("3271010101010001", 4, 4); got != "3271********0001" {
		t.Fatalf("mask = %s", got)
	}
	if got := MaskMiddle("123", 4, 4); got != "***" {
		t.Fatalf("short mask = %s", got)
	}
}
