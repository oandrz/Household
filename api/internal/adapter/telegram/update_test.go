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
