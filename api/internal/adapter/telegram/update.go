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
	} `json:"chat"`
}

// StartCommand is a /start carrying the deep-link payload the browser minted.
type StartCommand struct {
	ChatID  int64
	Payload string
}

// ParseStart returns false for everything that is not a /start, including
// updates with no message at all. The switch has a default that ignores rather
// than one that guesses: this value arrives from a third party, so the rule is
// the same as for a database column -- refuse what you did not construct.
func ParseStart(u Update) (StartCommand, bool) {
	if u.Message == nil {
		return StartCommand{}, false
	}
	command, payload, _ := strings.Cut(strings.TrimSpace(u.Message.Text), " ")
	switch command {
	case "/start":
		return StartCommand{ChatID: u.Message.Chat.ID, Payload: strings.TrimSpace(payload)}, true
	default:
		return StartCommand{}, false
	}
}
