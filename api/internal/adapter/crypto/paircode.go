package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// PairCodes draws the four digits an owner and their partner compare by eye
// before the owner admits them (ADR 11).
//
// crypto/rand rather than math/rand even though the code grants nothing and
// is never accepted by any endpoint: a predictable code would let someone
// watching the owner's screen over their shoulder -- or a future change
// that did start accepting it -- turn a display into a credential. The
// boring, obvious source costs nothing here.
type PairCodes struct{}

// NewCode returns exactly four decimal digits, leading zeros kept: "0007"
// is a valid code and must render as four characters, because the owner is
// matching it character by character against a phone.
func (PairCodes) NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", fmt.Errorf("draw pairing code: %w", err)
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}
