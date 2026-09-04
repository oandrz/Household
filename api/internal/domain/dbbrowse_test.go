package domain_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The type rule is the one that survives: every token in this schema is
// stored as bytea, so a bytea column -- or an array of them -- added by a
// migration years from now is redacted before its author has heard of this
// file. It is not a total guarantee, and the two rows at the bottom of the
// table below say exactly where it stops. The name rules exist because the
// type rule is not complete even today -- users.password_hash is text.
//
// The pairs matter: information_schema reports the CATEGORY in data_type for
// arrays ("ARRAY") and for domains and extension types ("USER-DEFINED"), so
// half these rows carry a data_type that is not a type name at all. That is
// the whole reason udt_name is an argument.
func TestColumnIsRedacted(t *testing.T) {
	cases := []struct {
		name     string
		column   string
		dataType string
		udtName  string
		want     bool
	}{
		{"a session token", "token_hash", "bytea", "bytea", true},
		{"a magic link token", "token_hash", "bytea", "bytea", true},
		{"a telegram nonce", "nonce_hash", "bytea", "bytea", true},
		{"any bytea at all, whatever it is called", "avatar", "bytea", "bytea", true},
		{"an array of bytea, which data_type only calls ARRAY", "blobs", "ARRAY", "_bytea", true},
		{"the one credential that is not bytea", "password_hash", "text", "text", true},
		{"a column merely mentioning a password", "password_set_at", "timestamptz", "timestamptz", true},
		{"an api secret", "webhook_secret", "text", "text", true},
		{"upper case is still a hash", "TOKEN_HASH", "BYTEA", "BYTEA", true},
		{"an ordinary column", "display_name", "text", "text", false},
		{"an ordinary amount", "amount_minor", "bigint", "int8", false},
		{"a column whose name merely ends in hash-like text", "hashtag", "text", "text", false},
		{"an id", "household_id", "uuid", "uuid", false},
		{"an array of something ordinary is still ordinary", "capabilities", "ARRAY", "_text", false},
		{"an extension type data_type cannot name", "email", "USER-DEFINED", "citext", false},
		// The gap, pinned rather than described. A domain reports its own
		// name in udt_name, and resolving that back to bytea needs
		// pg_type.typbasetype, which this stdlib-only package cannot read.
		// What catches this column instead is adapter/postgres's schema
		// sweep, which resolves base types through the catalogue. If someone
		// ever teaches this function to resolve domains, this row is the one
		// that goes red and should then be flipped to true.
		{"a domain over bytea, which this rule cannot reach", "token", "USER-DEFINED", "hearth_token", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.ColumnIsRedacted(c.column, c.dataType, c.udtName); got != c.want {
				t.Fatalf("ColumnIsRedacted(%q, %q, %q) = %v, want %v",
					c.column, c.dataType, c.udtName, got, c.want)
			}
		})
	}
}

// The markers must be distinguishable from each other and from an empty
// string, because [][]string cannot carry the difference any other way.
func TestTheTwoMarkersAreDistinct(t *testing.T) {
	if domain.RedactedCell == domain.NullCell {
		t.Fatal("the redacted and null markers are the same string")
	}
	if domain.RedactedCell == "" || domain.NullCell == "" {
		t.Fatal("a marker is empty, which is exactly what it exists to distinguish from")
	}
}
