package crypto_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
)

func TestNewCodeIsAlwaysFourDigits(t *testing.T) {
	codes := crypto.PairCodes{}
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := codes.NewCode()
		if err != nil {
			t.Fatalf("NewCode: %v", err)
		}
		if len(code) != 4 {
			t.Fatalf("code %q is %d characters, want 4 -- a leading zero must be kept", code, len(code))
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("code %q is not four decimal digits", code)
			}
		}
		seen[code] = true
	}
	// Not a statistical test, just a smoke alarm: a constant or a badly
	// seeded source would show up here as a handful of values.
	if len(seen) < 100 {
		t.Fatalf("500 draws produced only %d distinct codes; the source is not random", len(seen))
	}
}
