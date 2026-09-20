// Package telegram is the adapter that owns Hearth's Telegram dependency. It
// talks to Telegram's Bot API over outbound HTTPS only -- there is no webhook
// and no inbound route, so nothing in this package faces the internet.
package telegram

import "strings"

// Update is the subset of Telegram's Update object this product reads. Every
// other field Telegram sends is deliberately ignored: a bot that parses only
// what it acts on cannot be surprised by a payload shape it did not expect.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
		// Type is "private", "group", "supergroup" or "channel". Only
		// "private" is a single person, and every write keyed on a chat id
		// -- the Telegram binding, the pending-spend map, a knock -- assumes
		// one person. In a group the chat is everyone in it: any member
		// could confirm another member's spend, and a sign-in link sent to
		// the chat would be posted to the room (security review
		// 2026-09-19, finding 2).
		Type string `json:"type"`
	} `json:"chat"`
	// From is absent on a channel post, so this is a pointer and every
	// reader must handle nil. Read for display only -- the confirm screen
	// names the chat that redeemed a link -- never to decide anything:
	// a username is chosen by its owner and Telegram lets it change.
	From *User `json:"from"`
}

type User struct {
	// ID is compared with Chat.ID and nothing else. In a private chat the
	// two are the same number; anywhere else they differ, which is the
	// second gate behind Chat.Type.
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// senderName is the @username, or "" when Telegram sent none. It never falls
// back to FirstName: the confirm screen (decision 7,
// docs/adr/0010-binding-a-chat-needs-a-confirm.md) renders this value as
// "@<name>", and that is the *only* evidence a member gets that the chat
// which redeemed their link is really theirs. A first name is attacker-
// chosen and not unique -- a chat with no @username and a first name of
// "andreas" would render as "@andreas", indistinguishable from the real
// handle, which forges the one piece of evidence the confirm step exists to
// give. "" is a legitimate value the confirm screen renders honestly as "a
// Telegram chat with no username" -- not an error.
func senderName(m *Message) string {
	if m.From == nil {
		return ""
	}
	return m.From.Username
}

// isPrivateChatWithItsOwner answers whether this message came from one
// person's own one-to-one chat with the bot.
//
// Two gates, deliberately, because each fails differently. Chat.Type is
// Telegram's own answer and is checked with a switch whose default refuses,
// so a chat kind this build has never heard of is refused rather than
// guessed at (CLAUDE.md: fail closed on values you did not construct).
// From.ID == Chat.ID is the arithmetic that holds only in a private chat,
// and it still holds if Telegram ever adds a private-like type we would
// otherwise have to enumerate.
func isPrivateChatWithItsOwner(m *Message) bool {
	switch m.Chat.Type {
	case "private":
		return m.From != nil && m.From.ID == m.Chat.ID
	default:
		return false
	}
}

// StartCommand is a /start carrying the deep-link payload the browser minted.
type StartCommand struct {
	ChatID   int64
	Payload  string
	Username string // Telegram's @name; "" when Telegram sent none. Never a first name -- see senderName.
}

// ParseStart returns false for everything that is not a /start, including
// updates with no message at all. The switch has a default that ignores rather
// than one that guesses: this value arrives from a third party, so the rule is
// the same as for a database column -- refuse what you did not construct.
func ParseStart(u Update) (StartCommand, bool) {
	if u.Message == nil {
		return StartCommand{}, false
	}
	if !isPrivateChatWithItsOwner(u.Message) {
		return StartCommand{}, false
	}
	command, payload, _ := strings.Cut(strings.TrimSpace(u.Message.Text), " ")
	switch command {
	case "/start":
		return StartCommand{
			ChatID:   u.Message.Chat.ID,
			Payload:  strings.TrimSpace(payload),
			Username: senderName(u.Message),
		}, true
	default:
		return StartCommand{}, false
	}
}
