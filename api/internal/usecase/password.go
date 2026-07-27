package usecase

import "errors"

// minPasswordLength is the floor for every password this application accepts:
// invite acceptance and sign-up alike. It was minInvitePasswordLength in
// invite.go, which named the one caller that existed rather than the rule.
//
// The design document's create-household card says "At least 10 characters".
// That copy is wrong for this codebase, not the other way round -- 12 is what
// InviteService.Accept has always enforced and what
// adapter/http/errors.go's PASSWORD_TOO_SHORT message already tells a caller.
// Two different floors across the two ways of creating an account is how a
// defect gets in, so the copy changes and the rule does not.
const minPasswordLength = 12

// maxPasswordLength is the ceiling applied everywhere a caller-supplied
// password reaches PasswordHasher. argon2id's cost scales with the size of the
// string it hashes, so with no upper bound a caller could force an arbitrarily
// expensive hash by submitting a multi-megabyte password -- uncapped CPU cost
// fronted directly by an unauthenticated HTTP endpoint. 256 characters is far
// beyond any legitimate human-chosen or generator-produced password.
const maxPasswordLength = 256

// ErrPasswordTooShort and ErrPasswordTooLong are usecase sentinels rather than
// domain ones because domain has no notion of a password at all.
var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 256 characters")
)

// validatePassword is the single gate for a chosen password. Both
// InviteService.Accept and SignupService.Complete call it, so the two account
// creation paths cannot drift.
//
// AuthService.SignIn deliberately does NOT call this. It enforces the same
// ceiling privately (see verifyPassword in auth.go) and must never surface a
// distinguishable sentinel: a too-long password at sign-in has to fail exactly
// like a wrong one.
func validatePassword(plain string) error {
	if len(plain) < minPasswordLength {
		return ErrPasswordTooShort
	}
	if len(plain) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}
