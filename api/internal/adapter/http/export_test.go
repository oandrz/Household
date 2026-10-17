package httpadapter

import "time"

// The API tests live in package httpadapter_test and cannot see unexported
// names. These give them the limits production runs with, so a test asserts
// on the real number instead of a copy that can drift from it.
const (
	SignUpCompletionsPerIPPerWindow = signUpCompletionsPerIPPerWindow
	InviteAcceptsPerIPPerWindow     = inviteAcceptsPerIPPerWindow
)

// LimitsThatNeverBind is for router tests that are not about the household
// limits: they send hundreds of requests from one household, which the
// production limits would refuse.
var LimitsThatNeverBind = HouseholdLimits{
	PerSecond: 1e9, Burst: 1 << 30, InFlight: 1 << 10, MaxWaiting: 1 << 10, MaxWait: time.Minute,
}
