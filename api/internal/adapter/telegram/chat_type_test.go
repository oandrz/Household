package telegram

import "testing"

// privateUpdate is the shape Telegram sends for a one-to-one chat: the chat
// id and the sender id are the same number, and the type is "private".
func privateUpdate(text string) Update {
	u := Update{UpdateID: 1, Message: &Message{Text: text}}
	u.Message.Chat.ID = 4242
	u.Message.Chat.Type = "private"
	u.Message.From = &User{ID: 4242, Username: "jane_t"}
	return u
}

// groupUpdate is a group: a negative chat id, a "group" type, and a sender
// who is one of many members rather than the chat itself.
func groupUpdate(text string) Update {
	u := Update{UpdateID: 2, Message: &Message{Text: text}}
	u.Message.Chat.ID = -100500
	u.Message.Chat.Type = "group"
	u.Message.From = &User{ID: 4242, Username: "jane_t"}
	return u
}

func TestParseStartAcceptsOnlyAPrivateChat(t *testing.T) {
	if _, ok := ParseStart(privateUpdate("/start inv_abc")); !ok {
		t.Fatal("a private /start must parse")
	}
	for name, u := range map[string]Update{
		"group":        groupUpdate("/start inv_abc"),
		"supergroup":   withChatType(groupUpdate("/start inv_abc"), "supergroup"),
		"channel":      withChatType(groupUpdate("/start inv_abc"), "channel"),
		"empty type":   withChatType(privateUpdate("/start inv_abc"), ""),
		"unknown type": withChatType(privateUpdate("/start inv_abc"), "secret_new_kind"),
	} {
		if _, ok := ParseStart(u); ok {
			t.Errorf("%s: /start must be refused", name)
		}
	}
}

// A "private" chat whose sender is somebody else is refused too: the chat id
// is what every downstream write keys on, so it must be the person's own.
func TestParseStartRefusesASenderWhoIsNotTheChat(t *testing.T) {
	u := privateUpdate("/start inv_abc")
	u.Message.From = &User{ID: 9999, Username: "someone_else"}
	if _, ok := ParseStart(u); ok {
		t.Fatal("a private chat whose From.ID is not the chat id must be refused")
	}
	u.Message.From = nil
	if _, ok := ParseStart(u); ok {
		t.Fatal("a message with no From must be refused")
	}
}

func TestParseCommandAcceptsOnlyAPrivateChat(t *testing.T) {
	if _, ok := ParseCommand(privateUpdate("/balance")); !ok {
		t.Fatal("a private /balance must parse")
	}
	if _, ok := ParseCommand(groupUpdate("/balance")); ok {
		t.Fatal("a group /balance must be refused")
	}
	if _, ok := ParseCommand(groupUpdate("lunch 12.50")); ok {
		t.Fatal("group free text must be refused")
	}
}

func withChatType(u Update, chatType string) Update {
	u.Message.Chat.Type = chatType
	return u
}
