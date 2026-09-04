package domain

import "strings"

// The two markers a browsed cell carries instead of a value.
//
// Guillemets, and not a bare word, so that a real value reading "redacted"
// cannot be mistaken for a withheld one -- and so the screen has something
// specific to explain in its legend. NullCell exists because RowPage carries
// [][]string: without it, a SQL NULL and an empty text column would both
// arrive as "" and a reader would conclude they are the same thing. In this
// schema they are not, and the difference is sometimes the bug being
// investigated (users.email is NULL for a member who signed in through
// Telegram and never gave an address, and empty on nobody -- a magic link is
// SENT to an address, so a magic-link member necessarily has one).
//
// The two markers never compete for the same cell: redaction is decided in
// the SELECT list, so a redacted column's value is never fetched and a NULL
// in one still reads RedactedCell. users.password_hash is the example to
// avoid reaching for here -- it is NULL for every member who has never set a
// password, and it renders RedactedCell for all of them anyway, because
// ColumnIsRedacted matches its _hash suffix. Pick a nullable column that is
// NOT redacted when looking for NullCell on a real screen.
const (
	RedactedCell = "«redacted»"
	NullCell     = "«null»"
)

// redactedColumns is the explicit denylist: columns that are secret in a way
// neither their type nor their name reveals.
//
// It is empty on purpose. A denylist pre-filled with guesses becomes the
// thing people trust, and then the type rule below -- the only one that
// covers a column nobody has thought of yet -- stops being maintained. Every
// entry added here must carry a comment saying why the first two rules
// missed it.
var redactedColumns = map[string]bool{}

// ColumnIsRedacted reports whether a column's values must never be rendered.
// name, dataType and udtName all come from information_schema.columns, so all
// three are compared case-insensitively: nothing here may depend on how a
// migration happened to spell them.
//
// Two type strings rather than one, because data_type is not always a type
// name. For two families it reports the CATEGORY instead: an array reports
// "ARRAY" and a domain or an extension type reports "USER-DEFINED". udt_name
// carries the real name in both cases -- "_bytea" for bytea[], the domain's
// own name for a domain. So bytea is matched on both columns, and a caller
// holding only one of them still gets the plain case right.
//
// The three rules are deliberately ordered by how much they can be relied on.
// The type rule is first because it is the one that survives a schema this
// file has never seen: every token in Hearth is stored as bytea, so a bytea
// column -- or an array of them -- added by a migration years from now is
// redacted before its author has heard of this file.
//
// What the type rule does NOT cover, said here because an overstated
// guarantee is worse than a known gap: a DOMAIN over bytea
// (CREATE DOMAIN token AS bytea) reports its own name in udt_name, and
// resolving that back to bytea needs pg_type.typbasetype -- a catalogue this
// stdlib-only package cannot reach. Such a column is caught only by rule 2 or
// rule 3. What catches it instead is a test rather than this function:
// adapter/postgres's schema sweep resolves every column's base type through
// the catalogue and goes red the day a migration introduces one.
//
// The name rules exist because the type rule is not complete even for today's
// schema -- an Argon2 hash is a self-describing string, so users.password_hash
// is text. The denylist is the escape hatch for anything the first two miss.
func ColumnIsRedacted(name, dataType, udtName string) bool {
	lowerName := strings.ToLower(name)
	lowerType := strings.ToLower(dataType)
	lowerUDT := strings.ToLower(udtName)

	switch {
	case lowerType == "bytea", lowerUDT == "bytea", lowerUDT == "_bytea":
		return true
	case strings.HasSuffix(lowerName, "_hash"), strings.HasSuffix(lowerName, "_secret"):
		return true
	case strings.Contains(lowerName, "password"):
		return true
	default:
		return redactedColumns[lowerName]
	}
}
