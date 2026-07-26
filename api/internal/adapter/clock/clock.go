// Package clock provides the production implementation of the Clock port.
// Tests inject a fixed clock instead.
package clock

import "time"

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }
