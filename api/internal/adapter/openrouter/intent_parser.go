// Package openrouter is the adapter that owns Hearth's dependency on
// OpenRouter, a hosted gateway to open-weight models. It has one job:
// reading a chat sentence into a transaction intent (usecase.IntentParser),
// for nothing on OpenRouter's ":free" models. It speaks the OpenAI
// chat-completions dialect over plain net/http, so any host that speaks it
// (Ollama, vLLM, Groq) is a base URL away; only OpenRouter is wired today.
// A Claude adapter existed for a day (anthropic-sdk-go, `claude-opus-5`)
// and was removed on 2026-09-08 when the owner chose to run on free
// models; git has it if a paid model is ever wanted back. Nothing it returns is written without the
// person confirming it first -- see adapter/telegram/commands.go.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/intent"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

const (
	// DefaultBaseURL is OpenRouter itself; tests and other hosts override it.
	DefaultBaseURL = "https://openrouter.ai/api/v1"
	// maxTokens leaves room for models that spend reasoning tokens before
	// the tool call; a max_tokens stop yields no call, which reads as
	// "none".
	maxTokens = 4096
	// requestTimeout bounds one parse. The Commander also caps the context,
	// but a free-tier queue can stall for minutes and this client must never
	// hold the poller that long on its own.
	requestTimeout = 30 * time.Second
	// maxModels is OpenRouter's own cap on the fallback list. Checked at
	// construction so a fourth id fails the boot, not every message.
	maxModels = 3
	// maxErrorBody is how much of an error response the adapter reads for
	// its message; the rest is not a log line's business.
	maxErrorBody = 4 << 10
)

// IntentParser is one key and an ordered list of models.
type IntentParser struct {
	apiKey  string
	models  []string
	baseURL string
	client  *http.Client
}

// Option configures a parser; production passes none.
type Option func(*IntentParser)

// WithBaseURL points the parser at another OpenAI-compatible host. Tests use
// it for a fake server.
func WithBaseURL(u string) Option { return func(p *IntentParser) { p.baseURL = u } }

// NewIntentParser builds a parser for one key and one or more model ids,
// comma-separated, tried in order. Free models are rate-limited upstream
// minute to minute, each on its own schedule, so one id alone means "could
// not read that" for as long as that provider is busy; OpenRouter's
// `models` fallback list moves to the next in the same request. No model
// is defaulted here on purpose: the set of free, tool-capable models
// changes month to month, so the choice lives in configuration where it can
// change without a release.
func NewIntentParser(apiKey, models string, opts ...Option) (*IntentParser, error) {
	var ids []string
	for _, m := range strings.Split(models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			ids = append(ids, m)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("openrouter: OPENROUTER_MODEL names no model")
	}
	if len(ids) > maxModels {
		return nil, fmt.Errorf("openrouter: OPENROUTER_MODEL names %d models; OpenRouter accepts at most %d in a fallback list", len(ids), maxModels)
	}
	p := &IntentParser{apiKey: apiKey, models: ids, baseURL: DefaultBaseURL, client: &http.Client{Timeout: requestTimeout}}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Request and response shapes: only the fields this adapter sends or reads.
// Nothing dialect-specific beyond them is sent (no strict, no
// parallel_tool_calls, no reasoning), because OpenRouter forwards unknown
// parameters to the upstream provider and some of them refuse.
type chatRequest struct {
	Model string `json:"model"`
	// Models is OpenRouter's fallback list: the request runs on the first
	// that is not refusing. Omitted when there is only one.
	Models     []string      `json:"models,omitempty"`
	MaxTokens  int           `json:"max_tokens"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools"`
	ToolChoice any           `json:"tool_choice"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
					// Arguments is a JSON string per the dialect, but some
					// hosts return an object; RawMessage lets the reader
					// accept both.
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ParseIntent asks the model to call log_transaction and reads the call.
// tool_choice is forced to the function: with "auto", small open-weight
// models tend to answer in prose instead, and prose reads as "none" -- every
// sentence would come back "I could not read that". No tool call at all is
// still "none", so a host that ignores the forcing cannot make the bot write.
func (p *IntentParser) ParseIntent(ctx context.Context, in usecase.ParseIntentInput) (usecase.Intent, error) {
	var fallbacks []string
	if len(p.models) > 1 {
		fallbacks = p.models
	}
	body, err := json.Marshal(chatRequest{
		Model:     p.models[0],
		Models:    fallbacks,
		MaxTokens: maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: intent.SystemPrompt(in)},
			{Role: "user", Content: in.Text},
		},
		Tools: []chatTool{{Type: "function", Function: toolFunction{
			Name:        intent.ToolName,
			Description: intent.ToolDescription,
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           intent.Properties(),
				"required":             intent.Required(),
				"additionalProperties": false,
			},
		}}},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": intent.ToolName}},
	})
	if err != nil {
		return usecase.Intent{}, fmt.Errorf("openrouter request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return usecase.Intent{}, fmt.Errorf("openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		// A transport error from net/http quotes the URL and the address;
		// the key is in neither, but the rule is the telegram client's: a
		// log line names the operation and the kind of failure, never the
		// request. Timeout or not is the one fact an operator acts on.
		return usecase.Intent{}, fmt.Errorf("openrouter chat.completions: %s", transportFailure(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return usecase.Intent{}, fmt.Errorf("openrouter chat.completions: %s", errorMessage(resp))
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return usecase.Intent{}, fmt.Errorf("openrouter response: %w", err)
	}
	if len(out.Choices) == 0 {
		return usecase.Intent{Kind: "none"}, nil
	}
	for _, call := range out.Choices[0].Message.ToolCalls {
		if call.Function.Name != intent.ToolName {
			continue
		}
		read, err := intent.ReadArguments(argumentsJSON(call.Function.Arguments))
		if err != nil {
			return usecase.Intent{}, fmt.Errorf("openrouter %w", err)
		}
		return read, nil
	}
	return usecase.Intent{Kind: "none"}, nil
}

// argumentsJSON accepts the dialect's JSON-encoded string and the object
// some hosts send instead, and hands the reader plain JSON either way.
func argumentsJSON(raw json.RawMessage) []byte {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []byte(s)
	}
	return raw
}

// errorMessage is the status plus the provider's own message, and nothing
// else from the body: request and workspace ids are for the provider's
// dashboard, not this log.
func errorMessage(resp *http.Response) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if json.Unmarshal(raw, &body) == nil && body.Error.Message != "" {
		return fmt.Sprintf("%d %s", resp.StatusCode, body.Error.Message)
	}
	return fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

// transportFailure reduces a net/http error to the two words that matter.
func transportFailure(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timed out"
	}
	return "connection failed"
}
