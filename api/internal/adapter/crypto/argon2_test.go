package crypto_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestHashThenVerify(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	encoded, err := h.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("encoded = %q, want the argon2id PHC format", encoded)
	}
	if ok, err := h.Verify(context.Background(), "correct horse battery staple", encoded); err != nil || !ok {
		t.Fatalf("Verify of the correct password = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := h.Verify(context.Background(), "wrong password", encoded); err != nil || ok {
		t.Fatalf("Verify of the wrong password = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	first, _ := h.Hash(context.Background(), "same")
	second, _ := h.Hash(context.Background(), "same")

	if first == second {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	// Real salt and hash fields from a genuine encoding, reused below to
	// build otherwise-well-formed strings that are malformed in exactly one
	// field at a time.
	valid, err := h.Hash(context.Background(), "reference password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	parts := strings.Split(valid, "$")
	if len(parts) != 6 {
		t.Fatalf("a genuine encoding must have 6 '$'-separated parts, got %d: %q", len(parts), valid)
	}
	salt, hash := parts[4], parts[5]

	cases := map[string]string{
		"empty string":        "",
		"not a hash at all":   "not-a-hash",
		"truncated params":    "$argon2id$v=19$m=1",
		"wrong algorithm tag": "$bcrypt$whatever",
		"zero time cost":      "$argon2id$v=19$m=65536,t=0,p=2$" + salt + "$" + hash,
		"zero parallelism":    "$argon2id$v=19$m=65536,t=3,p=0$" + salt + "$" + hash,
		"zero memory cost":    "$argon2id$v=19$m=0,t=3,p=2$" + salt + "$" + hash,
		"empty salt field":    "$argon2id$v=19$m=65536,t=3,p=2$$" + hash,
		"non-empty but wrong-length hash field": "$argon2id$v=19$m=65536,t=3,p=2$" + salt + "$" +
			base64.RawStdEncoding.EncodeToString([]byte("too short to be a real key")),
		// The bug this guards against: Verify once derived its comparison
		// key's length from this field instead of the hasher's fixed keyLen,
		// so an empty field asked argon2 for a zero-length key and compared
		// it against nothing -- which can only "succeed". Don't "simplify"
		// away the len(want) == h.keyLen check.
		"empty hash field": "$argon2id$v=19$m=65536,t=3,p=2$" + salt + "$",
	}

	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			// (false, nil), not an error: malformed input is a refusal,
			// and an error would be read as "no check ran, try again".
			if ok, err := h.Verify(context.Background(), "anything", encoded); err != nil || ok {
				t.Fatalf("Verify of %q (%s) = (%v, %v), want (false, nil)", encoded, name, ok, err)
			}
		})
	}
}
