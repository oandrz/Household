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
// investigated (users.email is NULL for a member who has only ever signed in
// with a magic link, and empty on nobody).
//
// The two markers never compete for the same cell: redaction is decided in
// the SELECT list, so a redacted column's value is never fetched and a NULL
// in one still reads RedactedCell. users.password_hash is the example to
// avoid reaching for here -- it is NULL for exactly those magic-link members,
// and it renders RedactedCell for every one of them, because ColumnIsRedacted
// matches its _hash suffix. Pick a nullable column that is NOT redacted when
// looking for NullCell on a real screen.
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
// name and dataType come from information_schema, so both are compared
// case-insensitively: nothing here may depend on how a migration happened to
// spell them.
//
// The three rules are deliberately ordered by how much they can be relied on.
// The type rule is first because it is the one that survives a schema this
// file has never seen: every token in Hearth is stored as bytea. The name
// rules exist because that is not complete -- an Argon2 hash is a
// self-describing string, so users.password_hash is text. The denylist is the
// escape hatch for anything the first two would miss.
func ColumnIsRedacted(name, dataType string) bool {
	lowerName := strings.ToLower(name)
	lowerType := strings.ToLower(dataType)

	switch {
	case lowerType == "bytea":
		return true
	case strings.HasSuffix(lowerName, "_hash"), strings.HasSuffix(lowerName, "_secret"):
		return true
	case strings.Contains(lowerName, "password"):
		return true
	default:
		return redactedColumns[lowerName]
	}
}
