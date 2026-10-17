package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func TestCheckTextLengthCountsCharactersNotBytes(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"exactly the limit in ASCII", strings.Repeat("a", 80), true},
		{"one over in ASCII", strings.Repeat("a", 81), false},
		// 80 runes but 240 bytes: len() would refuse this.
		{"exactly the limit in CJK", strings.Repeat("中", 80), true},
		{"one over in CJK", strings.Repeat("中", 81), false},
		// 80 runes, 320 bytes.
		{"exactly the limit in emoji", strings.Repeat("😀", 80), true},
		// Counted as given: a service that stores the value untrimmed must
		// not pass a value the database CHECK then refuses.
		{"surrounding spaces count", strings.Repeat("a", 78) + "   ", false},
	}
	for _, c := range cases {
		err := domain.CheckTextLength(domain.TextName, c.value, domain.MaxNameLen)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestTextTooLongErrorCarriesFieldAndLimit(t *testing.T) {
	err := domain.CheckTextLength(domain.TextDescription, strings.Repeat("x", 201), domain.MaxTransactionDescriptionLen)
	var tooLong *domain.TextTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("err = %v, want *TextTooLongError", err)
	}
	if tooLong.Field != domain.TextDescription || tooLong.Max != 200 {
		t.Fatalf("got %+v", tooLong)
	}
	if !errors.Is(err, domain.ErrTextTooLong) {
		t.Fatal("errors.Is(err, ErrTextTooLong) = false")
	}
}

func TestTheApprovedTextLimits(t *testing.T) {
	if domain.MaxNameLen != 80 || domain.MaxTransactionDescriptionLen != 200 ||
		domain.MaxNoteLen != 500 || domain.MaxHoldingUnitLen != 20 {
		t.Fatal("text limits differ from the approved 80 / 200 / 500 / 20")
	}
}
