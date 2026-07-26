package crypto_test

import (
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestHashThenVerify(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("encoded = %q, want the argon2id PHC format", encoded)
	}
	if !h.Verify("correct horse battery staple", encoded) {
		t.Fatal("Verify rejected the correct password")
	}
	if h.Verify("wrong password", encoded) {
		t.Fatal("Verify accepted the wrong password")
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	first, _ := h.Hash("same")
	second, _ := h.Hash("same")

	if first == second {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	h := crypto.NewArgon2Hasher(3, 64*1024, 2)

	for _, encoded := range []string{"", "not-a-hash", "$argon2id$v=19$m=1", "$bcrypt$whatever"} {
		if h.Verify("anything", encoded) {
			t.Fatalf("Verify accepted %q", encoded)
		}
	}
}
