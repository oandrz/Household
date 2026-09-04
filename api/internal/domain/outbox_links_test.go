package domain_test

import (
	"reflect"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The fixtures are Hearth's four real message bodies (adapter/mail/smtp.go)
// plus the cases that body text alone would never produce. Two of them are
// constructed rather than captured and the reason is in their names: a real
// token ends in "-" or "_" only about one time in thirty-two, so a captured
// sample would pass a broken strip set by luck.
func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name string
		text string
		html string
		want []string
	}{
		{
			name: "magic link email",
			text: "Hi Chris,\n\nHere is your sign-in link:\n\nhttps://oink.mywire.org/sign-in/magic?token=abc123\n\nIt expires in 15 minutes.\n",
			want: []string{"https://oink.mywire.org/sign-in/magic?token=abc123"},
		},
		{
			name: "invite email",
			text: "Andreas has invited you to join Oentoro on Hearth.\n\nhttps://oink.mywire.org/invite/xyz789\n",
			want: []string{"https://oink.mywire.org/invite/xyz789"},
		},
		{
			name: "a full stop after a URL is not part of it",
			text: "Open https://oink.mywire.org/invite/xyz789.",
			want: []string{"https://oink.mywire.org/invite/xyz789"},
		},
		{
			name: "a token ending in a hyphen keeps its last character",
			text: "https://oink.mywire.org/invite/AbC-",
			want: []string{"https://oink.mywire.org/invite/AbC-"},
		},
		{
			name: "a token ending in an underscore keeps its last character",
			text: "https://oink.mywire.org/sign-in/magic?token=AbC_",
			want: []string{"https://oink.mywire.org/sign-in/magic?token=AbC_"},
		},
		{
			name: "the same URL twice appears once, in source order",
			text: "https://a.example/1 then https://b.example/2 then https://a.example/1",
			want: []string{"https://a.example/1", "https://b.example/2"},
		},
		{
			name: "a mailto is not a link the operator can hand over",
			text: "Reply to mailto:hearth@example.com or open https://a.example/1",
			want: []string{"https://a.example/1"},
		},
		{
			name: "html is read only when there is no text part, with entities unescaped",
			html: `<p>Open <a href="https://a.example/x?y=1&amp;z=2">your link</a></p>`,
			want: []string{"https://a.example/x?y=1&z=2"},
		},
		{
			name: "a text part wins over an html part",
			text: "https://text.example/1",
			html: `<a href="https://html.example/2">x</a>`,
			want: []string{"https://text.example/1"},
		},
		{
			name: "a message with no links is an empty slice, never nil",
			text: "Your household is ready.",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.ExtractLinks(tt.text, tt.html)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractLinks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
