// Package crypto implements the hashing and token ports. Parameters live in
// one place so they can be raised without touching any caller.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Hasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen int
}

// NewArgon2Hasher takes its cost parameters from configuration so they can be
// raised without a code change, as the spec requires. Callers pass
// cfg.Argon2Time, cfg.Argon2MemoryKiB and cfg.Argon2Threads.
func NewArgon2Hasher(time uint32, memoryKiB uint32, threads uint8) *Argon2Hasher {
	return &Argon2Hasher{time: time, memory: memoryKiB, threads: threads, keyLen: 32, saltLen: 16}
}

func (h *Argon2Hasher) Hash(plain string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, h.time, h.memory, h.threads, h.keyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify is constant-time in the comparison and tolerant of malformed input:
// any parse failure is a rejection, never a panic.
func (h *Argon2Hasher) Verify(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
