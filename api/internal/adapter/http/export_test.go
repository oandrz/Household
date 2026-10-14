package httpadapter

// The API tests live in package httpadapter_test and cannot see unexported
// names. These give them the limits production runs with, so a test asserts
// on the real number instead of a copy that can drift from it.
const (
	SignUpCompletionsPerIPPerWindow = signUpCompletionsPerIPPerWindow
	InviteAcceptsPerIPPerWindow     = inviteAcceptsPerIPPerWindow
)
