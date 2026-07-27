package usecase

import (
	"regexp"
	"strings"
)

// maxSignupEmailLength bounds a sign-up address before isPlausibleEmail ever
// runs a regexp against it. RFC 5321 caps a mailbox at 254 characters end to
// end; this guard does not need to be more precise than that to do its job --
// it exists to stop a multi-kilobyte string from being counted and hashed at
// all, not to enforce the standard.
const maxSignupEmailLength = 254

// plausibleEmailPattern mirrors the frontend's PLAUSIBLE_EMAIL
// (web/src/features/auth/copy.ts): a local part and a domain part, each with
// no whitespace and no "@", joined by exactly one "@", with the domain
// containing a "." with something on both sides of it. It is not an RFC 5322
// validator and was never meant to be one.
var plausibleEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// isPlausibleEmail is a budget guard, not a correctness check. It exists so
// SignupService.Request can refuse an address like "" or "andreas" before
// spending a counted read or a signups row on it -- see Request's own doc
// comment for why that ordering matters. It is deliberately as loose as its
// frontend mirror: non-empty, an "@" with something on both sides, a "."
// somewhere in the domain with something on both sides of that too, no
// whitespace anywhere, and under a sane length. Tightening it further would
// reject legitimate but unusual-looking addresses while doing nothing to
// raise the cost of a loop, which is the only thing this function defends
// against -- it has no opinion on whether the address is real, reachable, or
// ever will be, only on whether it is worth spending budget to find out.
//
// Leading and trailing whitespace is trimmed before the check, matching the
// frontend's own email.trim() -- a pasted address with incidental surrounding
// whitespace is not "junk" in the sense this guard cares about. The trimmed
// value is used only for this check; Request still reads, counts, and writes
// under the address exactly as the caller supplied it.
func isPlausibleEmail(email string) bool {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || len(trimmed) > maxSignupEmailLength {
		return false
	}
	return plausibleEmailPattern.MatchString(trimmed)
}
