// This file holds the platform ports: time, hashing, tokens and outbound
// mail -- the infrastructure every slice leans on. ports.go lists every
// ports file.

package usecase

import (
	"context"
	"errors"
	"time"
)

type Clock interface {
	Now() time.Time
}

// PasswordHasher derives and checks password hashes. A derivation is the
// most expensive thing a stranger can ask this service to do, so an
// implementation may run only a few at once and make the rest wait.
//
// Both methods answer ErrHasherBusy when too many callers are already
// waiting, and ctx's own error when ctx ends before a derivation starts.
// In both cases nothing was checked: a caller must not treat either as a
// wrong password, or a flood of requests could turn every real sign-in into
// a strike against its household.
//
// Verify's bool is meaningful only when its error is nil. A malformed
// encoded hash is (false, nil): a refusal, not a fault.
//
// Full reports whether a caller arriving now would be refused. It is a hint
// taken a moment early, not a promise: the answer can change before Hash or
// Verify is called, and those still decide. A service asks it before doing
// anything else, so that a request it is about to refuse costs no database
// work. Without that, a flood of refused requests would queue for database
// connections instead of for the hasher, and real callers would wait behind
// them all the same.
type PasswordHasher interface {
	Hash(ctx context.Context, plain string) (string, error)
	Verify(ctx context.Context, plain, encoded string) (bool, error)
	Full() bool
}

// ErrHasherBusy is PasswordHasher's "ask again shortly": the queue for a
// derivation is full. The HTTP layer answers 503 for it.
var ErrHasherBusy = errors.New("password hasher is busy")

type TokenGenerator interface {
	NewToken() (raw string, hash []byte, err error)
	HashToken(raw string) []byte
}

type Mailer interface {
	SendMagicLink(ctx context.Context, to, name, url string) error
	SendInvite(ctx context.Context, to, name, inviterName, url string) error
	// SendSignupLink mails the create-household link. There is no name
	// parameter: at sign-up-request time nobody has told us one, and inventing
	// a greeting from the local part of the address would read worse than
	// having none.
	SendSignupLink(ctx context.Context, to, url string) error
	// SendSignupForExistingAccount mails "you already have an account" with no
	// token. It is as load-bearing as SendSignupLink, not a courtesy: if only
	// the fresh-address branch sent mail, the *absence* of an email would tell
	// anyone who can observe the mailbox that the address is registered, which
	// is the oracle the identical 202 exists to prevent.
	SendSignupForExistingAccount(ctx context.Context, to, signInURL string) error
}
