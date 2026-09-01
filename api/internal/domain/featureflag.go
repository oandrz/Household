package domain

import "fmt"

// A Capability answers *who may use this*. A Flag answers *whether this
// install has it at all*. A route may carry both, and neither substitutes for
// the other: turning a flag off hides a feature from everybody, including an
// owner who holds every capability.
type Flag string

const (
	// FlagSignupsOpen gates POST /auth/sign-up and the public sign-up screen,
	// so registration can be closed without a redeploy.
	FlagSignupsOpen Flag = "signups_open"
	// FlagTelegramSignIn gates the Telegram routes, which until now were
	// reachable or not purely by whether a bot was configured (ADR 4).
	FlagTelegramSignIn Flag = "telegram_sign_in"
	// FlagNotificationDelivery gates sending on the notification preferences.
	// Default off: the preferences are real, nothing sends them yet, and a
	// flag that is on for something that cannot happen is a lie.
	FlagNotificationDelivery Flag = "notification_delivery"
	// FlagFamilyCalendar gates an unbuilt page. It exists now so that
	// dark-shipping is exercised before it is needed in anger.
	FlagFamilyCalendar Flag = "family_calendar"
)

// FlagDefinition is one flag as this build knows it. Default is what a fresh
// install does with no override rows at all.
type FlagDefinition struct {
	Flag        Flag
	Description string
	Default     bool
}

// AllFlags is the whole registry. Adding a flag is one const above and one
// line here; nothing else in the system needs to learn about it.
func AllFlags() []FlagDefinition {
	return []FlagDefinition{
		{FlagSignupsOpen, "Accept new sign-ups from the public form.", true},
		{FlagTelegramSignIn, "Offer Telegram as a sign-in and sign-up channel.", true},
		{FlagNotificationDelivery, "Actually send the notifications members have asked for.", false},
		{FlagFamilyCalendar, "Show the Family calendar page.", false},
	}
}

// ParseFlag turns a key from a database column or a request into a Flag,
// refusing anything this build does not define.
func ParseFlag(s string) (Flag, error) {
	for _, def := range AllFlags() {
		if def.Flag == Flag(s) {
			return def.Flag, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownFlag, s)
}

// FlagSet is one household's resolved answer for every flag this build
// defines. Every defined key is present, so a caller never has to decide what
// a missing key means.
type FlagSet map[Flag]bool

// Enabled answers false for a flag this build does not define, so a typo in a
// caller closes a route rather than opening one.
func (f FlagSet) Enabled(flag Flag) bool { return f[flag] }

// Strings renders the set for the wire, where JSON keys are strings.
func (f FlagSet) Strings() map[string]bool {
	out := make(map[string]bool, len(f))
	for flag, enabled := range f {
		out[string(flag)] = enabled
	}
	return out
}

// ResolveFlags answers, for one household, what every flag in defs is set to.
//
// Precedence: a household override beats a global override, which beats the
// compile-time default. Keys neither map's caller could have validated are
// simply not consulted -- the result is built by walking defs, never by
// walking the overrides -- so an override row naming a flag this build does
// not define can never enable anything. That row can exist: `key` has no
// foreign key, deliberately, because the registry is compile-time.
//
// Pass a nil household map for a caller with no household (the pre-auth
// routes); household overrides are meaningless before there is a household and
// must never be treated as "on".
func ResolveFlags(defs []FlagDefinition, global, household map[Flag]bool) FlagSet {
	out := make(FlagSet, len(defs))
	for _, def := range defs {
		enabled := def.Default
		if v, ok := global[def.Flag]; ok {
			enabled = v
		}
		if v, ok := household[def.Flag]; ok {
			enabled = v
		}
		out[def.Flag] = enabled
	}
	return out
}
