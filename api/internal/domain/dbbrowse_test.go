package domain_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The type rule is the one that survives: every token in this schema is
// stored as bytea, so a secret column added by a migration in 2027 is
// redacted before its author has heard of this file. The name rules exist
// because the type rule is not complete -- users.password_hash is text.
func TestColumnIsRedacted(t *testing.T) {
	cases := []struct {
		name     string
		column   string
		dataType string
		want     bool
	}{
		{"a session token", "token_hash", "bytea", true},
		{"a magic link token", "token_hash", "bytea", true},
		{"a telegram nonce", "nonce_hash", "bytea", true},
		{"any bytea at all, whatever it is called", "avatar", "bytea", true},
		{"the one credential that is not bytea", "password_hash", "text", true},
		{"a column merely mentioning a password", "password_set_at", "timestamptz", true},
		{"an api secret", "webhook_secret", "text", true},
		{"upper case is still a hash", "TOKEN_HASH", "BYTEA", true},
		{"an ordinary column", "display_name", "text", false},
		{"an ordinary amount", "amount_minor", "bigint", false},
		{"a column whose name merely ends in hash-like text", "hashtag", "text", false},
		{"an id", "household_id", "uuid", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.ColumnIsRedacted(c.column, c.dataType); got != c.want {
				t.Fatalf("ColumnIsRedacted(%q, %q) = %v, want %v", c.column, c.dataType, got, c.want)
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
