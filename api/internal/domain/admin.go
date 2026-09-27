package domain

import "time"

// PlatformAdmin is an operator of this install, not a household member,
// and carries no permissions. A future admin level gets its own field
// here, not a reuse of Role or Capabilities (a different axis; identity.go).
type PlatformAdmin struct {
	UserID    string
	Note      string
	CreatedAt time.Time
}
