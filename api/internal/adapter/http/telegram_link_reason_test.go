package httpadapter

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// TestTelegramLinkReasonMessage pins the one place a usecase.TelegramLinkStatus
// refusal code becomes the sentence a member reads. It is unexported and has
// no HTTP surface of its own -- handleTelegramLinkStatus's test coverage
// proves the route compiles and returns 200, not that the two codes map to
// the two sentences the confirm 409s use. That is what this test is for, and
// it needs no container: it is a pure function.
func TestTelegramLinkReasonMessage(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "chat taken matches the confirm 409's sentence",
			code: usecase.TelegramLinkReasonChatTaken,
			want: telegramChatTakenMessage,
		},
		{
			name: "already linked matches the confirm 409's sentence",
			code: usecase.TelegramLinkReasonAlreadyLinked,
			want: telegramAlreadyLinkedMessage,
		},
		{
			name: "empty code (every non-refused status) answers empty",
			code: "",
			want: "",
		},
		{
			// Fail closed on a code this function was not written to expect,
			// the same rule a switch over any value it did not construct
			// follows elsewhere in this codebase: never guess at a sentence
			// for a case nobody named. Reason is `omitempty` on the wire, so
			// "" here means the panel's own `reason ?? "..."` fallback
			// renders instead of a raw code leaking to the screen.
			name: "an unrecognised code answers empty rather than leaking the raw code",
			code: "something-nobody-named",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := telegramLinkReasonMessage(tc.code); got != tc.want {
				t.Fatalf("telegramLinkReasonMessage(%q) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}
