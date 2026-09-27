package httpadapter

import (
	"testing"
	"time"
)

// TestCursorRoundTrips pins encodeCursor/decodeCursor as inverses -- the
// property the keyset pager depends on, since decodeCursor feeds
// TransactionFilter.CursorDate/CursorID directly and a drifted round-trip
// would silently reorder or skip a page rather than error.
func TestCursorRoundTrips(t *testing.T) {
	occurredOn := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	id := "b6f1a0d2-8f0a-4a11-9c3a-9d6f9e6b0f2a"

	cursor := encodeCursor(occurredOn, id)
	gotDate, gotID, ok := decodeCursor(cursor)
	if !ok {
		t.Fatalf("decodeCursor(%q) failed to parse a cursor this file just encoded", cursor)
	}
	if !gotDate.Equal(occurredOn) {
		t.Fatalf("decoded date = %v, want %v", gotDate, occurredOn)
	}
	if gotID != id {
		t.Fatalf("decoded id = %q, want %q", gotID, id)
	}
}

// TestDecodeCursorRejectsMalformedInput is the disproof half: a cursor that
// silently returns zero rows is indistinguishable from reaching the end of
// the ledger, so decodeCursor must refuse both a bad date shape and a date
// paired with a non-uuid id rather than build a filter that matches nothing.
func TestDecodeCursorRejectsMalformedInput(t *testing.T) {
	cases := []string{
		"",
		"not-a-cursor",
		"2026-03-18",           // missing the ":id" half entirely
		"2026-03-18x deadbeef", // wrong separator where the colon must be
		"2026-13-99:some-id",   // not a real calendar date
		"2026-03-18:garbage",   // valid date, but the id half is not a uuid
	}
	for _, raw := range cases {
		if _, _, ok := decodeCursor(raw); ok {
			t.Fatalf("decodeCursor(%q) = ok, want rejected", raw)
		}
	}
}
