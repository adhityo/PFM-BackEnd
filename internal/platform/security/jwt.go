package security

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AccessClaims are the claims carried by an access token.
type AccessClaims struct {
	Role       string `json:"role,omitempty"`
	ClientUUID string `json:"client_uuid,omitempty"`
	jwt.RegisteredClaims
}

// JWTSigner issues and verifies EdDSA access tokens.
type JWTSigner struct {
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

// LoadEd25519PrivateKey reads a PKCS#8 PEM Ed25519 private key
// (openssl genpkey -algorithm ed25519).
func LoadEd25519PrivateKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path comes from trusted configuration
	if err != nil {
		return nil, fmt.Errorf("read jwt key: %w", err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("jwt key: bukan PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwt key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("jwt key: bukan Ed25519")
	}
	return priv, nil
}

func NewJWTSigner(priv ed25519.PrivateKey, issuer, audience string, ttl time.Duration) *JWTSigner {
	pub, _ := priv.Public().(ed25519.PublicKey)
	return &JWTSigner{priv: priv, pub: pub, issuer: issuer, audience: audience, ttl: ttl, now: time.Now}
}

// TTL returns the access token lifetime.
func (s *JWTSigner) TTL() time.Duration { return s.ttl }

// Sign issues an access token for the given user.
func (s *JWTSigner) Sign(userUUID uuid.UUID, role string, clientUUID *uuid.UUID) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	claims := AccessClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userUUID.String(),
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	if clientUUID != nil {
		claims.ClientUUID = clientUUID.String()
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(s.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}
	return tok, exp, nil
}

// Verify parses and validates an access token. Only EdDSA is accepted.
func (s *JWTSigner) Verify(token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return s.pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, errors.New("subject tidak valid")
	}
	return claims, nil
}
