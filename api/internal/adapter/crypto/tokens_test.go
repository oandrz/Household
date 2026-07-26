package crypto_test

import (
	"bytes"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestNewTokenIsUrlSafeAndUnique(t *testing.T) {
	g := crypto.NewTokenGenerator()

	first, firstHash, err := g.NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	second, _, _ := g.NewToken()

	if first == second {
		t.Fatal("two tokens must differ")
	}
	if len(first) < 32 {
		t.Fatalf("token is only %d characters", len(first))
	}
	for _, r := range first {
		if r == '+' || r == '/' || r == '=' {
			t.Fatalf("token %q is not URL-safe", first)
		}
	}
	if !bytes.Equal(firstHash, g.HashToken(first)) {
		t.Fatal("HashToken must reproduce the hash returned by NewToken")
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	g := crypto.NewTokenGenerator()

	if !bytes.Equal(g.HashToken("abc"), g.HashToken("abc")) {
		t.Fatal("HashToken must be deterministic")
	}
	if bytes.Equal(g.HashToken("abc"), g.HashToken("abd")) {
		t.Fatal("different tokens must hash differently")
	}
}
