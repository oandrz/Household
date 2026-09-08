package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fakeMessages is the Messages API as this adapter uses it: it records the
// request and answers a canned tool_use.
func fakeMessages(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("api key header missing")
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

const toolUseReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"tool_use","content":[
  {"type":"tool_use","id":"tu_1","name":"log_transaction","input":{"kind":"expense","amount":"84.50","description":"groceries","account":"DBS Savings","category":"Groceries"}}
],"usage":{"input_tokens":10,"output_tokens":5}}`

func TestParseIntentAsksForOneStrictToolCallAndReadsIt(t *testing.T) {
	srv, got := fakeMessages(t, toolUseReply)
	p := NewIntentParser("sk-test", option.WithBaseURL(srv.URL))
	intent, err := p.ParseIntent(context.Background(), usecase.ParseIntentInput{
		Text: "spent 84.50 on groceries at DBS", Accounts: []string{"DBS Savings", "OCBC"}, Categories: []string{"Groceries"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := usecase.Intent{Kind: "expense", Amount: "84.50", Description: "groceries", Account: "DBS Savings", Category: "Groceries"}
	if intent != want {
		t.Fatalf("got %+v want %+v", intent, want)
	}

	req := *got
	if req["model"] != "claude-opus-5" {
		t.Fatalf("model %v", req["model"])
	}
	tools := req["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "log_transaction" || tool["strict"] != true {
		t.Fatalf("tool %v", tool)
	}
	schema := tool["input_schema"].(map[string]any)
	if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 5 {
		t.Fatalf("schema must be closed and fully required: %v", schema)
	}
	choice := req["tool_choice"].(map[string]any)
	if choice["type"] != "auto" || choice["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice %v", choice)
	}
	if req["output_config"].(map[string]any)["effort"] != "low" {
		t.Fatalf("effort %v", req["output_config"])
	}
	if _, has := req["thinking"]; has {
		t.Fatalf("thinking must be left at the model's default, not sent: %v", req["thinking"])
	}
	system := req["system"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(system, "DBS Savings; OCBC") || !strings.Contains(system, "Groceries") {
		t.Fatalf("system prompt must carry the names: %s", system)
	}
}

func TestParseIntentTreatsNoToolCallAndBadKindsAsNone(t *testing.T) {
	for name, reply := range map[string]string{
		"text only": `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"end_turn","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
		"none":      `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t","name":"log_transaction","input":{"kind":"none","amount":"","description":"","account":"","category":""}}],"usage":{"input_tokens":1,"output_tokens":1}}`,
		"bad kind":  `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t","name":"log_transaction","input":{"kind":"transfer","amount":"5","description":"x","account":"","category":""}}],"usage":{"input_tokens":1,"output_tokens":1}}`,
		"refusal":   `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"refusal","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		srv, _ := fakeMessages(t, reply)
		p := NewIntentParser("sk-test", option.WithBaseURL(srv.URL))
		intent, err := p.ParseIntent(context.Background(), usecase.ParseIntentInput{Text: "hello"})
		if err != nil || intent.Kind != "none" {
			t.Errorf("%s: intent=%+v err=%v, want kind none", name, intent, err)
		}
	}
}

func TestParseIntentErrorsNameTheOperationNotTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, 401)
	}))
	defer srv.Close()
	p := NewIntentParser("sk-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	_, err := p.ParseIntent(context.Background(), usecase.ParseIntentInput{Text: "x"})
	if err == nil || !strings.HasPrefix(err.Error(), "anthropic messages.new:") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "sk-test") {
		t.Fatalf("error leaked the URL or key: %v", err)
	}
}
