package telegram

import "testing"

func TestParseStart(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantOK  bool
		payload string
	}{
		{name: "start with a payload", text: "/start abc123", wantOK: true, payload: "abc123"},
		{name: "start with no payload", text: "/start", wantOK: true, payload: ""},
		{name: "start with trailing space", text: "/start  abc123  ", wantOK: true, payload: "abc123"},
		{name: "another command", text: "/help", wantOK: false},
		{name: "ordinary chatter", text: "hello", wantOK: false},
		{name: "empty", text: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := Update{UpdateID: 1}
			u.Message = &Message{Text: tc.text}
			u.Message.Chat.ID = 55

			got, ok := ParseStart(u)
			if ok != tc.wantOK {
				t.Fatalf("ParseStart(%q) ok = %v, want %v", tc.text, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Payload != tc.payload {
				t.Fatalf("payload = %q, want %q", got.Payload, tc.payload)
			}
			if got.ChatID != 55 {
				t.Fatalf("chatID = %d, want 55", got.ChatID)
			}
		})
	}
}

// An update with no message at all -- an edited message, a callback query, a
// channel post -- must be ignored, not panic.
func TestParseStartIgnoresUpdatesWithNoMessage(t *testing.T) {
	if _, ok := ParseStart(Update{UpdateID: 9}); ok {
		t.Fatal("ParseStart on a message-less update returned ok, want false")
	}
}

func TestParseStartReadsTheSenderName(t *testing.T) {
	u := Update{UpdateID: 7, Message: &Message{Text: "/start abc"}}
	u.Message.Chat.ID = 501
	u.Message.From = &User{Username: "andreas", FirstName: "Andreas"}

	got, ok := ParseStart(u)
	if !ok || got.Username != "andreas" {
		t.Fatalf("ParseStart() = %+v, %v; want Username \"andreas\"", got, ok)
	}
}

// A first name is not an identifier: Telegram lets anyone set it to anything,
// including someone else's real @username. If ParseStart fell back to it,
// a chat with no @username and a first name of "andreas" would render on the
// confirm screen (telegramChatLabel) as "@andreas" -- indistinguishable from
// the genuine handle, and the confirm step's only piece of evidence
// (docs/adr/0010-binding-a-chat-needs-a-confirm.md) would be forgeable by
// whoever set their first name. So a first name with no @username must read
// as "", not as a name.
func TestParseStartRefusesAFirstNameAsTheSenderName(t *testing.T) {
	u := Update{UpdateID: 71, Message: &Message{Text: "/start abc"}}
	u.Message.Chat.ID = 511
	u.Message.From = &User{FirstName: "andreas"}

	got, ok := ParseStart(u)
	if !ok || got.Username != "" {
		t.Fatalf("ParseStart() = %+v, %v; want Username \"\" -- a first name must never stand in for the sender's @username", got, ok)
	}
}

// Telegram omits `from` on a channel post. A nil there must not panic the
// poller: the update is still a /start, it just names nobody.
func TestParseStartToleratesAMissingSender(t *testing.T) {
	u := Update{UpdateID: 8, Message: &Message{Text: "/start abc"}}
	u.Message.Chat.ID = 502

	got, ok := ParseStart(u)
	if !ok || got.Username != "" {
		t.Fatalf("ParseStart() = %+v, %v; want ok with an empty Username", got, ok)
	}
}
