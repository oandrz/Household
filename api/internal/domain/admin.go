package domain

import "time"

// PlatformAdmin is an operator of this install: the person who runs it, not a
// member of any one household. It is deliberately empty of permissions. There
// are no admin levels today, and adding one later means adding a field here
// rather than reinterpreting Role or Capabilities, which belong to a different
// axis entirely (see identity.go).
type PlatformAdmin struct {
	UserID    string
	Note      string
	CreatedAt time.Time
}
