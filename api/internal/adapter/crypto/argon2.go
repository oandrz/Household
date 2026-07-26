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
// any parse failure, or any parsed-but-unusable parameter (zero cost
// parameters, an empty salt, or a stored key of the wrong length), is a
// rejection, never a panic and never an accept.
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
	// Sscanf succeeds on a numeric-but-zero field ("t=0" is not a parse
	// failure), but argon2.IDKey panics on time < 1 or threads < 1
	// ("number of rounds too small" / "parallelism degree too low"), and a
	// zero memory cost is nonsensical input rather than merely a cheap one.
	// All three are rejected here, before IDKey is ever called.
	if time < 1 || threads < 1 || memory < 1 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	// An empty salt decodes without error, but Hash never produces one — a
	// zero-length salt in a stored hash can only be corruption or tampering.
	if len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	// The stored key's length must match what Hash actually produces before
	// any comparison is made. Deriving with a length taken from the input
	// (uint32(len(want))) is the bug this guards: an empty want field would
	// otherwise ask argon2.IDKey to derive a zero-length key — which this
	// version of golang.org/x/crypto/argon2 does not even do safely, it
	// panics via a nil blake2b digest — and a want of any other wrong
	// length would derive a key sized to match it, defeating the point of a
	// fixed-length comparison. Rejecting a length mismatch up front, before
	// deriving anything, closes the whole class in one check.
	if len(want) != int(h.keyLen) {
		return false
	}

	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, h.keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}
