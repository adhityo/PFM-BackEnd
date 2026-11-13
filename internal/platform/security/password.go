// Package security holds password hashing, token handling and PII masking.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the argon2id cost parameters.
type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// DefaultArgon2Params follows PRD §12.1 (m=64 MiB, t=3, p=2).
var DefaultArgon2Params = Argon2Params{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLen: 16, KeyLen: 32}

var errInvalidHash = errors.New("format hash password tidak valid")

// PasswordHasher hashes and verifies passwords in PHC string format.
type PasswordHasher struct {
	p Argon2Params
	// dummy is verified when a user does not exist so login timing does not
	// reveal which usernames are registered.
	dummy string
}

func NewPasswordHasher(p Argon2Params) *PasswordHasher {
	h := &PasswordHasher{p: p}
	dummy, err := h.Hash("dummy-password-for-timing")
	if err != nil {
		panic(err)
	}
	h.dummy = dummy
	return h
}

// Hash returns $argon2id$v=19$m=...,t=...,p=...$salt$hash.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.p.Iterations, h.p.MemoryKiB, h.p.Parallelism, h.p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.p.MemoryKiB, h.p.Iterations,
		h.p.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether the hash
// should be upgraded because its parameters differ from the current ones.
func (h *PasswordHasher) Verify(password, encoded string) (match bool, needsRehash bool, err error) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	other := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(key)))
	match = subtle.ConstantTimeCompare(key, other) == 1
	needsRehash = p.MemoryKiB != h.p.MemoryKiB || p.Iterations != h.p.Iterations || p.Parallelism != h.p.Parallelism
	return match, needsRehash, nil
}

// VerifyDummy burns the same CPU time as a real verification.
func (h *PasswordHasher) VerifyDummy(password string) {
	_, _, _ = h.Verify(password, h.dummy)
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	var p Argon2Params
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, errInvalidHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return p, nil, nil, errInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, errInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return p, nil, nil, errInvalidHash
	}
	return p, salt, key, nil
}
