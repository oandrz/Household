package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Text limits, in characters as a person sees them. Count with
// utf8.RuneCountInString, never len(): len counts bytes, and a household
// writing Chinese would get a third of what the field promises. Postgres's
// char_length counts the same way, so the database CHECKs on these columns
// and these rules never disagree.
const (
	MaxNameLen                   = 80
	MaxTransactionDescriptionLen = 200
	MaxNoteLen                   = 500
	MaxHoldingUnitLen            = 20
)

// TextField names which kind of text was too long, so the HTTP layer can
// word the refusal. It is a code, not copy.
type TextField string

const (
	TextName        TextField = "name"
	TextDescription TextField = "description"
	TextNote        TextField = "note"
	TextUnit        TextField = "unit"
)

// TextTooLongError says Field is longer than Max characters.
type TextTooLongError struct {
	Field TextField
	Max   int
}

func (e *TextTooLongError) Error() string {
	return fmt.Sprintf("%s is longer than %d characters", e.Field, e.Max)
}

func (e *TextTooLongError) Unwrap() error { return ErrTextTooLong }

// CheckTextLength refuses value if it is longer than max characters once
// surrounding spaces are trimmed -- the same trim every required-text rule
// here already applies.
func CheckTextLength(field TextField, value string, max int) error {
	if utf8.RuneCountInString(strings.TrimSpace(value)) > max {
		return &TextTooLongError{Field: field, Max: max}
	}
	return nil
}
