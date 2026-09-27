package domain

import "strings"

// The two markers a browsed cell carries instead of a value.
//
// Guillemets, not a bare word, so a real value reading "redacted" can't be
// mistaken for a withheld one. NullCell exists because RowPage carries
// [][]string: without it, a SQL NULL and an empty text column would both
// arrive as "", though the difference is sometimes the bug under
// investigation (users.email is NULL for a Telegram-only member; a magic-link
// member always has one, since the link is SENT to it).
//
// The two markers never compete for one cell: redaction is decided in the
// SELECT list, so a NULL in a redacted column still reads RedactedCell. Don't
// test NullCell against users.password_hash for this reason -- it's NULL for
// anyone without a password, but reads RedactedCell anyway, since
// ColumnIsRedacted matches its _hash suffix. Pick a nullable column that
// isn't redacted instead.
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
// name, dataType and udtName come from information_schema.columns, compared
// case-insensitively so nothing here depends on how a migration spelled them.
//
// Two type strings, not one, because data_type isn't always a type name: for
// an array it reports "ARRAY" and for a domain or extension type
// "USER-DEFINED", while udt_name carries the real name in both cases. bytea
// is matched on both columns, so a caller holding only one still gets it
// right.
//
// The three rules are ordered by how much they can be relied on. The type
// rule goes first because it survives a schema this file has never seen:
// every token in Hearth is stored as bytea, so a future bytea column is
// redacted before its author has heard of this file. It does NOT cover a
// DOMAIN over bytea, which needs pg_type.typbasetype -- a catalogue this
// stdlib-only package can't reach; adapter/postgres's schema sweep test
// catches that case instead. The name rules exist because the type rule isn't
// complete even today -- users.password_hash is text -- and the denylist is
// the last-resort escape hatch for anything the first two miss.
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
