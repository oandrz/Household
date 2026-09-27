package usecase

import (
	"regexp"
	"strings"
)

// maxSignupEmailLength bounds a sign-up address before isPlausibleEmail
// ever runs a regexp against it. RFC 5321 caps a mailbox at 254 characters;
// this guard exists to stop a multi-kilobyte string from being counted and
// hashed, not to enforce the standard.
const maxSignupEmailLength = 254

// plausibleEmailPattern mirrors the frontend's PLAUSIBLE_EMAIL
// (web/src/features/auth/copy.ts): a local part and a domain part, neither
// with whitespace or "@", joined by one "@", with a "." somewhere in the
// domain. It is not an RFC 5322 validator and was never meant to be one.
var plausibleEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// isPlausibleEmail is a budget guard, not a correctness check: it exists so
// SignupService.Request can refuse an address like "" or "andreas" before
// spending a counted read or a signups row on it (see Request's own doc
// comment). It is as loose as its frontend mirror: non-empty, an "@" with
// something on both sides, a "." somewhere in the domain, no whitespace,
// under a sane length. Tightening it further would reject legitimate but
// unusual addresses without raising the cost of a loop -- the only thing
// this function defends against; it has no opinion on whether an address
// is real or reachable.
//
// Leading and trailing whitespace is trimmed before the check, matching the
// frontend's email.trim() -- incidental surrounding whitespace is not
// "junk". Request still reads, counts and writes under the address exactly
// as the caller supplied it.
func isPlausibleEmail(email string) bool {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || len(trimmed) > maxSignupEmailLength {
		return false
	}
	return plausibleEmailPattern.MatchString(trimmed)
}
