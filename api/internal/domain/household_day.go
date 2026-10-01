package domain

import (
	"fmt"
	"time"
)

// A household keeps one calendar: its time zone decides which day "today" is
// for everyone in it, wherever they are and wherever the server sits (ADR 12).
// This file is the only place that turns an instant into that day.

// ParseTimezone loads an IANA time zone name such as "Asia/Singapore".
//
// It refuses "" and "Local" before asking the standard library, because
// time.LoadLocation accepts both without complaint: "" loads as UTC and
// "Local" as whatever zone the server runs in. Either would hand a household
// the server's calendar while looking like a stored choice.
func ParseTimezone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimezone, name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimezone, name)
	}
	return loc, nil
}

// TodayIn is the calendar day it is right now in the given time zone, returned
// as that day's midnight stamped UTC.
//
// The result is a date, not an instant. Don't pass now.In(loc) inward instead:
// some date rules read a value's own location (Period, StartableMonth) and
// others convert to UTC first (NextDue, IsOverdue), and a zoned instant gives
// those two families different days. A day stamped midnight UTC is the one
// shape both read the same way.
//
// An unknown zone is an error, never a fallback to UTC: a household silently
// on the wrong calendar is the defect a stored zone exists to remove.
func TodayIn(now time.Time, zone string) (time.Time, error) {
	loc, err := ParseTimezone(zone)
	if err != nil {
		return time.Time{}, err
	}
	year, month, day := now.In(loc).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC), nil
}

// IsAfterDay reports whether date falls on a later calendar day than today.
// It is the single test behind "a recorded fact may not be dated in the
// future": today itself is never after today, whatever the hour.
//
// Both values are read as the day they already name, in their own location,
// the way startOfDayUTC reads one. Comparing instants instead refuses this
// morning's purchase because the clock reads a later hour.
func IsAfterDay(date, today time.Time) bool {
	return startOfDayUTC(date).After(startOfDayUTC(today))
}
