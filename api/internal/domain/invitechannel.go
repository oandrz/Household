package domain

import "fmt"

// An InviteChannel is how an invite reaches the person it is for, stored in
// invites.channel. It's parsed, not cast, so an unrecognised value is
// refused rather than silently defaulted -- see ParseInviteChannel for why
// a request body never reaches this type directly.
type InviteChannel string

const (
	// ChannelEmail is an address, a mailed link, seven days. It is gated by
	// FlagEmailInvites, which is off by default while production mail cannot
	// leave the box (ADR 3).
	ChannelEmail InviteChannel = "email"
	// ChannelTelegram is a one-time t.me deep link the owner hands over,
	// with no address at all. It carries its knock on the same row
	// (ADR 11).
	ChannelTelegram InviteChannel = "telegram"
)

// AllInviteChannels is the whole set, so a caller enumerating channels
// cannot miss one that was added later.
func AllInviteChannels() []InviteChannel { return []InviteChannel{ChannelEmail, ChannelTelegram} }

// ParseInviteChannel turns a database column into an InviteChannel,
// refusing anything else including "". A request never reaches here: the
// HTTP layer has its own three-valued vocabulary (parseInviteChannelChoice),
// since a kid profile writes no invite row and so has no channel to store.
func ParseInviteChannel(s string) (InviteChannel, error) {
	for _, c := range AllInviteChannels() {
		if InviteChannel(s) == c {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownInviteChannel, s)
}
