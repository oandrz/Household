package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fakeChat is the chat-completions endpoint as this adapter uses it: it
// records the request and answers a canned body with a status.
func fakeChat(t *testing.T, status int, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-or-test" {
			t.Errorf("bearer missing, got %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

const toolCallReply = `{"id":"gen-1","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[
  {"id":"call_1","type":"function","function":{"name":"log_transaction","arguments":"{\"kind\":\"expense\",\"amount\":\"84.50\",\"description\":\"groceries\",\"account\":\"DBS Savings\",\"category\":\"Groceries\"}"}}
]}}]}`

func parse(t *testing.T, srv *httptest.Server, text string) (usecase.Intent, error) {
	t.Helper()
	p, err := NewIntentParser("sk-or-test", "some/model:free", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return p.ParseIntent(context.Background(), usecase.ParseIntentInput{
		Text: text, Accounts: []string{"DBS Savings", "OCBC"}, Categories: []string{"Groceries"},
	})
}

func TestParseIntentForcesOneToolCallAndReadsIt(t *testing.T) {
	srv, got := fakeChat(t, 200, toolCallReply)
	intent, err := parse(t, srv, "spent 84.50 on groceries at DBS")
	if err != nil {
		t.Fatal(err)
	}
	want := usecase.Intent{Kind: "expense", Amount: "84.50", Description: "groceries", Account: "DBS Savings", Category: "Groceries"}
	if intent != want {
		t.Fatalf("got %+v want %+v", intent, want)
	}

	req := *got
	if req["model"] != "some/model:free" {
		t.Fatalf("model %v", req["model"])
	}
	if _, ok := req["models"]; ok {
		t.Fatalf("one model means no fallback list, got %v", req["models"])
	}
	if req["max_tokens"].(float64) != 4096 {
		t.Fatalf("max_tokens %v", req["max_tokens"])
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "user" {
		t.Fatalf("messages %v", msgs)
	}
	if sys := msgs[0].(map[string]any)["content"].(string); !strings.Contains(sys, "DBS Savings; OCBC") || !strings.Contains(sys, "Groceries") {
		t.Fatalf("system prompt lacks the names: %s", sys)
	}
	if msgs[1].(map[string]any)["content"] != "spent 84.50 on groceries at DBS" {
		t.Fatalf("user message %v", msgs[1])
	}
	tools := req["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if len(tools) != 1 || fn["name"] != "log_transaction" {
		t.Fatalf("tools %v", tools)
	}
	params := fn["parameters"].(map[string]any)
	if params["additionalProperties"] != false || len(params["required"].([]any)) != 5 {
		t.Fatalf("schema must be closed with every field required: %v", params)
	}
	choice := req["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["function"].(map[string]any)["name"] != "log_transaction" {
		t.Fatalf("tool_choice must force the function, got %v", choice)
	}
	for _, forbidden := range []string{"strict", "parallel_tool_calls", "reasoning"} {
		if _, ok := req[forbidden]; ok {
			t.Errorf("%s must not be sent: some upstream providers refuse it", forbidden)
		}
	}
}

func TestProseInsteadOfAToolCallReadsAsNone(t *testing.T) {
	srv, _ := fakeChat(t, 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Hello! How can I help?"}}]}`)
	intent, err := parse(t, srv, "hello")
	if err != nil || intent.Kind != "none" {
		t.Fatalf("got %+v %v, want none", intent, err)
	}
}

func TestAnInventedKindReadsAsNone(t *testing.T) {
	srv, _ := fakeChat(t, 200, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"log_transaction","arguments":"{\"kind\":\"transfer\",\"amount\":\"5\",\"description\":\"x\",\"account\":\"\",\"category\":\"\"}"}}]}}]}`)
	intent, err := parse(t, srv, "moved 5 to savings")
	if err != nil || intent.Kind != "none" || intent.Amount != "" {
		t.Fatalf("got %+v %v, want none and nothing else", intent, err)
	}
}

func TestArgumentsSentAsAnObjectAreReadToo(t *testing.T) {
	srv, _ := fakeChat(t, 200, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"log_transaction","arguments":{"kind":"income","amount":"6500","description":"salary","account":"","category":""}}}]}}]}`)
	intent, err := parse(t, srv, "got paid 6500")
	if err != nil || intent.Kind != "income" || intent.Amount != "6500" {
		t.Fatalf("got %+v %v", intent, err)
	}
}

func TestMalformedArgumentsAreAnError(t *testing.T) {
	srv, _ := fakeChat(t, 200, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"log_transaction","arguments":"{\"kind\":"}}]}}]}`)
	if _, err := parse(t, srv, "x"); err == nil {
		t.Fatal("malformed arguments must surface as an error")
	}
}

func TestAProviderErrorNamesTheStatusAndMessageOnly(t *testing.T) {
	srv, _ := fakeChat(t, 402, `{"error":{"message":"Insufficient credits","code":402,"metadata":{"request_id":"req_secret_123"}}}`)
	_, err := parse(t, srv, "x")
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	for _, want := range []string{"openrouter chat.completions", "402", "Insufficient credits"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	for _, forbidden := range []string{"http", "sk-or-test", "req_secret_123"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("error %q must not carry %q", msg, forbidden)
		}
	}
}

func TestATransportErrorCarriesNoURL(t *testing.T) {
	p, err := NewIntentParser("sk-or-test", "m", WithBaseURL("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.ParseIntent(context.Background(), usecase.ParseIntentInput{Text: "x"})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "http") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error must not carry the request: %q", err)
	}
	if !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("error must say what kind of failure: %q", err)
	}
}

// Free models are rate-limited upstream on their own schedules; a
// comma-separated OPENROUTER_MODEL becomes OpenRouter's fallback list, so
// one request tries the next model instead of answering "could not read".
func TestACommaSeparatedModelListBecomesAFallbackList(t *testing.T) {
	srv, got := fakeChat(t, 200, toolCallReply)
	p, err := NewIntentParser("sk-or-test", " a/one:free, b/two:free ,c/three ", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ParseIntent(context.Background(), usecase.ParseIntentInput{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	req := *got
	if req["model"] != "a/one:free" {
		t.Fatalf("model must be the first, got %v", req["model"])
	}
	models := req["models"].([]any)
	if len(models) != 3 || models[1] != "b/two:free" || models[2] != "c/three" {
		t.Fatalf("models must be every id in order, trimmed: %v", models)
	}
}

// OpenRouter refuses a fallback list longer than three with a 400 on every
// request; the first live walk hit exactly that. Refuse at boot instead.
func TestMoreThanThreeModelsIsRefusedAtConstruction(t *testing.T) {
	if _, err := NewIntentParser("k", "a,b,c,d"); err == nil {
		t.Fatal("four models must be refused when the parser is built, not on every message")
	}
	if _, err := NewIntentParser("k", " , "); err == nil {
		t.Fatal("no model at all must be refused")
	}
	if _, err := NewIntentParser("k", "a,b,c"); err != nil {
		t.Fatalf("three models is the cap and must be accepted: %v", err)
	}
}
