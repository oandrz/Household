package usecase

import "context"

// IntentParser turns a sentence a person typed into a chat -- "spent 84.50
// on groceries at DBS" -- into the same fields the /spend grammar produces.
// It is a port because the implementation is a language model behind an API
// key; with none configured, the bot falls back to /help.
//
// A parser only reads intent, never writes: the Telegram Commander shows it
// back and writes only after the person confirms -- why Intent carries text
// fields, not ids, so the person can read what will be logged before it is.
type IntentParser interface {
	ParseIntent(ctx context.Context, in ParseIntentInput) (Intent, error)
}

// ParseIntentInput is the sentence plus the names the parser may pick from.
// Names, not ids: the model should echo a name the person would recognise,
// and resolution to an id happens afterwards by the same rule /spend uses.
type ParseIntentInput struct {
	Text       string
	Accounts   []string
	Categories []string
}

// Intent is what the parser understood. Kind "none" means "this was not a
// transaction" (a greeting, a question) and the bot says so rather than
// guessing. Amount is the person's own decimal text, parsed later in the
// account's currency by domain.ParseAmount -- the parser never does
// arithmetic.
type Intent struct {
	Kind        string // "expense" | "income" | "none"
	Amount      string
	Description string
	Account     string // one of Accounts, or ""
	Category    string // one of Categories, or ""
}
