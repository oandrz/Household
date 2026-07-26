package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Tokens issues opaque bearer strings for sessions, magic links and invites.
// Only the SHA-256 hash is ever persisted; the raw value lives in a cookie or
// an email and nowhere else.
type Tokens struct{}

func NewTokenGenerator() *Tokens { return &Tokens{} }

func (t *Tokens) NewToken() (string, []byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("read random bytes: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return raw, t.HashToken(raw), nil
}

func (t *Tokens) HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
