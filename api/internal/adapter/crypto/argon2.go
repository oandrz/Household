// Package crypto implements the hashing and token ports. Parameters live in
// one place so they can be raised without touching any caller.
package crypto

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type Argon2Hasher struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen int

	// slots bounds how many derivations run at once, since each holds
	// `memory` KiB (64 MiB by default) for its duration and sign-in runs one
	// per attempt, including a decoy for unknown addresses -- which must stay,
	// so an attempt cannot tell who has an account. Unbounded, ~60 parallel
	// sign-ins can exhaust a 4 GB box and get the API or Postgres killed.
	slots chan struct{}
	// queue bounds how many callers wait for a slot. Without it the line has
	// no end: a burst of a few hundred requests makes every sign-in behind
	// it wait for all of them. A caller who finds the line full is told
	// usecase.ErrHasherBusy at once.
	//
	// Don't go back to "wait, never refuse": the per-IP limiters stop one
	// address, not a burst from many, and a person told "busy, try again"
	// in a moment is better served than one left watching a spinner.
	queue chan struct{}
	// idKey is argon2.IDKey, held as a field only so a test can observe how
	// many derivations are running at once.
	idKey func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte
}

// NewArgon2Hasher takes its cost parameters from configuration so they can be
// raised without a code change. Callers pass cfg.Argon2Time,
// cfg.Argon2MemoryKiB and cfg.Argon2Threads.
func NewArgon2Hasher(time uint32, memoryKiB uint32, threads uint8) *Argon2Hasher {
	return &Argon2Hasher{
		time: time, memory: memoryKiB, threads: threads, keyLen: 32, saltLen: 16,
		slots: make(chan struct{}, maxConcurrentDerivations()),
		queue: make(chan struct{}, maxConcurrentDerivations()*waitersPerSlot),
		idKey: argon2.IDKey,
	}
}

// waitersPerSlot sizes the line behind each slot, and so the longest anyone
// waits: about waitersPerSlot derivations. At the default cost a derivation
// takes roughly 0.1 to 0.3 seconds on a small server, so eight keeps the
// worst wait under about three seconds. Raise ARGON2_TIME or
// ARGON2_MEMORY_KIB and that wait grows with them; lower this to match.
const waitersPerSlot = 8

// MinConcurrentDerivations is the fewest slots any box gets. It is exported
// so the HTTP layer's rate-limit test can check its per-address allowances
// against the smallest hasher they will ever sit in front of.
const MinConcurrentDerivations = 2

// maxConcurrentDerivations is one per CPU, with a floor of two. More than one
// per CPU adds memory without adding throughput -- argon2 is CPU-bound -- so
// on the 2-vCPU production box the ceiling is 2 x 64 MiB.
func maxConcurrentDerivations() int {
	return max(runtime.NumCPU(), MinConcurrentDerivations)
}

// derive runs one argon2id derivation inside a slot. It answers
// usecase.ErrHasherBusy when the line for a slot is full, and ctx's error
// when the caller goes away before reaching the front. Once a derivation has
// started it runs to the end -- argon2 cannot be interrupted -- and its
// answer stands.
func (h *Argon2Hasher) derive(ctx context.Context, plain string, salt []byte, time, memory uint32, threads uint8) ([]byte, error) {
	if err := h.acquireSlot(ctx); err != nil {
		return nil, err
	}
	defer func() { <-h.slots }()
	return h.idKey([]byte(plain), salt, time, memory, threads, h.keyLen), nil
}

// acquireSlot takes a free slot if there is one, and otherwise joins the
// line for one. Every path out of the line gives the place back.
func (h *Argon2Hasher) acquireSlot(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
	}

	select {
	case h.queue <- struct{}{}:
	default:
		return usecase.ErrHasherBusy
	}
	defer func() { <-h.queue }()

	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BusyThreads is the most goroutines this hasher can have computing at once:
// every slot, times the threads one derivation uses at the configured cost.
// The process's start-up reads it to leave the Go scheduler room for
// everything else.
func (h *Argon2Hasher) BusyThreads() int {
	return cap(h.slots) * int(h.threads)
}

// Full is true when every slot is taken and every place in line is too; see
// usecase.PasswordHasher.
func (h *Argon2Hasher) Full() bool {
	return len(h.slots) == cap(h.slots) && len(h.queue) == cap(h.queue)
}

func (h *Argon2Hasher) Hash(ctx context.Context, plain string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key, err := h.derive(ctx, plain, salt, h.time, h.memory, h.threads)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify is constant-time in the comparison and tolerant of malformed input:
// any parse failure, or any parsed-but-unusable parameter (zero cost
// parameters, an empty salt, or a stored key of the wrong length), is a
// rejection, never a panic and never an accept. A rejection is (false, nil);
// an error means no check ran at all -- see usecase.PasswordHasher.
func (h *Argon2Hasher) Verify(ctx context.Context, plain, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, nil
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, nil
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, nil
	}
	// Sscanf treats "t=0" as a valid parse, but argon2.IDKey panics on
	// time < 1 or threads < 1, and a zero memory cost is nonsensical rather
	// than merely cheap. All three are rejected here, before IDKey runs.
	if time < 1 || threads < 1 || memory < 1 {
		return false, nil
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, nil
	}
	// An empty salt decodes without error, but Hash never produces one — a
	// zero-length salt in a stored hash can only be corruption or tampering.
	if len(salt) == 0 {
		return false, nil
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, nil
	}
	// The stored key's length must match what Hash actually produces before
	// any comparison is made. This guards against deriving with a length
	// taken from the input (uint32(len(want))): an empty want would ask
	// argon2.IDKey to derive a zero-length key, which this version of
	// golang.org/x/crypto/argon2 panics on, and any other wrong length
	// would derive a key sized to match it, defeating the point of a
	// fixed-length comparison. Rejecting a length mismatch up front, before
	// deriving anything, closes the whole class.
	if len(want) != int(h.keyLen) {
		return false, nil
	}

	got, err := h.derive(ctx, plain, salt, time, memory, threads)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
