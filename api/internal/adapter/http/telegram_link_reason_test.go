package httpadapter

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// TestTelegramLinkReasonMessage pins the one place a usecase.TelegramLinkStatus
// refusal code becomes the sentence a member reads. handleTelegramLinkStatus's
// own test only proves the route returns 200, not that the codes map to the
// confirm 409 sentences -- this pure function needs no container to test that.
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
			// Fail closed: never guess a sentence for a code nobody named, the
			// same rule any switch over an unconstructed value follows here.
			// Reason is `omitempty` on the wire, so "" lets the panel's own
			// fallback render instead of a raw code leaking to the screen.
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
