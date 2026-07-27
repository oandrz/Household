package postgres

import (
	"testing"
	"unicode/utf8"
)

// initialOf used to be strings.ToUpper(name[:1]) -- a *byte* slice. For any
// name starting outside ASCII that takes the first byte of a multi-byte UTF-8
// rune, producing an invalid fragment that renders as the replacement
// character. There is no profile-edit endpoint, so the wrong initial was
// permanent. Two known adults never hit it; a public sign-up form does.
func TestInitialOfHandlesNonASCIINames(t *testing.T) {
	for _, tc := range []struct {
		name        string
		displayName string
		want        string
	}{
		{"ascii", "Andreas", "A"},
		{"accented latin", "Émile", "É"},
		{"cyrillic", "Дмитрий", "Д"},
		{"greek", "Ωμέγα", "Ω"},
		{"cjk has no case", "李明", "李"},
		{"leading whitespace is trimmed", "  Christine", "C"},
		{"already uppercase", "ANDREAS", "A"},
		{"empty falls back", "", "?"},
		{"whitespace only falls back", "   ", "?"},
		{"uppercase can grow a rune", "ßeta", "SS"},
		// language.Und, not language.Turkish: under Turkish case rules 'i' uppercases
		// to 'İ' (dotted). A display name's locale is unknown, so the root mapping is
		// the only defensible choice, and this pins it.
		{"turkish i uses root mapping", "istanbul", "I"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := initialOf(tc.displayName); got != tc.want {
				t.Fatalf("initialOf(%q) = %q, want %q", tc.displayName, got, tc.want)
			}
		})
	}
}

// The fragment the old implementation produced, spelled out, so this test
// documents the defect rather than only the fix.
func TestInitialOfNeverReturnsInvalidUTF8(t *testing.T) {
	for _, name := range []string{"Émile", "Дмитрий", "李明", "🙂nonymous"} {
		got := initialOf(name)
		if !utf8.ValidString(got) {
			t.Fatalf("initialOf(%q) = %q, which is not valid UTF-8", name, got)
		}
	}
}
