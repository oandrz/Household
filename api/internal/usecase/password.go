package usecase

import "errors"

// minPasswordLength is the floor for every password: invite acceptance and
// sign-up alike.
//
// The design doc says "At least 10 characters" -- don't change this constant
// to match it. 12 is what InviteService.Accept and the PASSWORD_TOO_SHORT
// message already enforce; the copy should change, not the rule.
const minPasswordLength = 12

// maxPasswordLength is the ceiling applied everywhere a password reaches
// PasswordHasher. argon2id's cost scales with input size, so with no cap an
// unauthenticated caller could force an expensive hash with a multi-megabyte
// password. 256 is far beyond any legitimate password.
const maxPasswordLength = 256

// ErrPasswordTooShort and ErrPasswordTooLong are usecase sentinels rather than
// domain ones because domain has no notion of a password at all.
var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 256 characters")
)

// validatePassword is the single gate for a chosen password, called by both
// InviteService.Accept and SignupService.Complete so the two paths cannot
// drift. AuthService.SignIn deliberately skips it: verifyPassword in auth.go
// enforces the same ceiling privately, so a too-long password fails exactly
// like a wrong one, with no distinguishable sentinel.
func validatePassword(plain string) error {
	if len(plain) < minPasswordLength {
		return ErrPasswordTooShort
	}
	if len(plain) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}
