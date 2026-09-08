// Package anthropic is the adapter that owns Hearth's dependency on the
// Claude API. It exists for one job: reading a chat sentence into a
// transaction intent (usecase.IntentParser). Nothing it returns is written
// without the person confirming it first -- see adapter/telegram/commands.go.
// The prompt, the tool and the reader of its arguments are shared with the
// openrouter adapter through adapter/intent; this file owns only the Claude
// request shape.
package anthropic

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/andreasoentoro/hearth/api/internal/adapter/intent"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// model is pinned by name here, once. Effort is low: the task is reading
// one short sentence into five fields, and lower effort on the current
// generation is more than enough for that.
const (
	model = "claude-opus-5"
	// 4096, not the 1024 a five-field tool call needs: on this model
	// thinking counts against max_tokens, and a max_tokens stop yields no
	// tool call, which this adapter would read as "none".
	maxTokens = 4096
)

type IntentParser struct {
	client sdk.Client
}

// NewIntentParser builds a parser for one API key. opts is for tests, which
// point the client at a fake server with option.WithBaseURL; production
// passes none. The key is never logged: errors below name the method, not
// the request.
func NewIntentParser(apiKey string, opts ...option.RequestOption) *IntentParser {
	all := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &IntentParser{client: sdk.NewClient(all...)}
}

// ParseIntent asks the model to call log_transaction with what it read. The
// tool schema is strict, so the arguments always validate; tool_choice stays
// auto (forced tool use is not available on every model) and the system
// prompt tells the model to call the tool once with kind "none" when the
// sentence is not a transaction. No tool call at all is treated as "none"
// too, so the bot never writes on a model that chose to chat instead.
func (p *IntentParser) ParseIntent(ctx context.Context, in usecase.ParseIntentInput) (usecase.Intent, error) {
	tool := sdk.ToolParam{
		Name:        intent.ToolName,
		Description: sdk.String(intent.ToolDescription),
		Strict:      sdk.Bool(true),
		InputSchema: sdk.ToolInputSchemaParam{
			Properties:  intent.Properties(),
			Required:    intent.Required(),
			ExtraFields: map[string]any{"additionalProperties": false},
		},
	}
	resp, err := p.client.Messages.New(ctx, sdk.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []sdk.TextBlockParam{{Text: intent.SystemPrompt(in)}},
		Messages:  []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock(in.Text))},
		Tools:     []sdk.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: sdk.ToolChoiceUnionParam{OfAuto: &sdk.ToolChoiceAutoParam{
			DisableParallelToolUse: sdk.Bool(true),
		}},
		OutputConfig: sdk.OutputConfigParam{Effort: sdk.OutputConfigEffortLow},
	})
	if err != nil {
		// The SDK's error carries the request URL; the message here names
		// the operation instead so a log line never carries a key.
		return usecase.Intent{}, fmt.Errorf("anthropic messages.new: %w", sanitize(err))
	}
	if resp.StopReason == sdk.StopReasonRefusal {
		return usecase.Intent{Kind: "none"}, nil
	}
	for _, block := range resp.Content {
		if tu, ok := block.AsAny().(sdk.ToolUseBlock); ok && tu.Name == intent.ToolName {
			read, err := intent.ReadArguments([]byte(tu.JSON.Input.Raw()))
			if err != nil {
				return usecase.Intent{}, fmt.Errorf("anthropic %w", err)
			}
			return read, nil
		}
	}
	return usecase.Intent{Kind: "none"}, nil
}

// sanitize strips anything URL-shaped from an SDK error. The Claude API does
// not put the key in the URL the way Telegram does, but the rule is the
// same one the telegram client follows: an error message is a log line, and
// a log line carries no request detail it does not need.
func sanitize(err error) error {
	msg := err.Error()
	if i := strings.Index(msg, "http"); i >= 0 {
		if j := strings.IndexAny(msg[i:], " \"\n"); j >= 0 {
			msg = msg[:i] + "<url>" + msg[i+j:]
		} else {
			msg = msg[:i] + "<url>"
		}
	}
	return fmt.Errorf("%s", msg)
}
