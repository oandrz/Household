// Package intent holds what every language-model adapter for
// usecase.IntentParser shares: the prompt, the one tool the model is asked
// to call, and the fail-closed reader of that tool's arguments. It exists
// because there are two such adapters (anthropic, openrouter) and a prompt
// that lives in two places drifts -- the reader in particular is the last
// line between a model's output and the ledger, and must be identical
// whichever model produced the output.
package intent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// ToolName is the single function the model is asked to call.
const ToolName = "log_transaction"

// ToolDescription is shown to the model beside the schema.
const ToolDescription = "Record what the person wants to log: one expense or income, or none if the message is not about logging money."

// Properties is the JSON-schema properties block of the tool's arguments.
// Every field is required and additional properties are refused; each
// adapter wraps this in its own dialect's envelope.
func Properties() map[string]any {
	return map[string]any{
		"kind":        map[string]any{"type": "string", "enum": []string{"expense", "income", "none"}},
		"amount":      map[string]any{"type": "string", "description": "The amount exactly as the person wrote it, digits and an optional decimal point only, e.g. \"84.50\". Empty when kind is none."},
		"description": map[string]any{"type": "string", "description": "What it was for, a few words. Empty when kind is none."},
		"account":     map[string]any{"type": "string", "description": "One of the account names given, verbatim, or empty if the person did not name one."},
		"category":    map[string]any{"type": "string", "description": "One of the category names given, verbatim, or empty if none clearly fits."},
	}
}

// Required lists every property: the model must fill each field, with an
// empty string where nothing applies, so the reader never has to guess
// whether a missing key meant "empty" or "forgot".
func Required() []string {
	return []string{"kind", "amount", "description", "account", "category"}
}

// SystemPrompt tells the model the job and the names it may use.
func SystemPrompt(in usecase.ParseIntentInput) string {
	var b strings.Builder
	b.WriteString("You read one chat message from a member of a household budgeting app and call log_transaction exactly once with what they want to log. ")
	b.WriteString("An expense is money spent; an income is money received. If the message is not about logging money, call the tool with kind \"none\" and the other fields empty. ")
	b.WriteString("Copy the amount exactly as written, without a currency symbol. Never invent an amount. ")
	b.WriteString("Only use an account or category name from the lists below, verbatim; leave the field empty otherwise.\n")
	fmt.Fprintf(&b, "Accounts: %s\n", strings.Join(in.Accounts, "; "))
	fmt.Fprintf(&b, "Categories: %s\n", strings.Join(in.Categories, "; "))
	return b.String()
}

// ReadArguments turns the tool call's JSON arguments into an Intent. It
// fails closed on the one value the model constructs that the code later
// switches on: any kind other than expense or income becomes "none", so a
// model that invents "transfer" or "refund" makes the bot say "I could not
// read that" rather than write something. Malformed JSON is an error, not
// "none", because that is the adapter's fault to surface, not the person's
// sentence.
func ReadArguments(raw []byte) (usecase.Intent, error) {
	var out struct {
		Kind, Amount, Description, Account, Category string
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return usecase.Intent{}, fmt.Errorf("tool arguments: %w", err)
	}
	switch out.Kind {
	case "expense", "income":
	default:
		return usecase.Intent{Kind: "none"}, nil
	}
	return usecase.Intent{
		Kind:        out.Kind,
		Amount:      strings.TrimSpace(out.Amount),
		Description: strings.TrimSpace(out.Description),
		Account:     strings.TrimSpace(out.Account),
		Category:    strings.TrimSpace(out.Category),
	}, nil
}
