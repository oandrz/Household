package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendMessagePostsTheChatAndText(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	if err := c.SendMessage(context.Background(), 4242, "hello"); err != nil {
		t.Fatalf("SendMessage() = %v, want nil", err)
	}
	if !strings.HasSuffix(gotPath, "/sendMessage") {
		t.Fatalf("path = %q, want it to end in /sendMessage", gotPath)
	}
	if gotBody["text"] != "hello" {
		t.Fatalf("text = %v, want %q", gotBody["text"], "hello")
	}
}

// Telegram answers 200 with ok:false for application-level failures, so a
// status check alone would treat a refused send as a success.
func TestSendMessageFailsWhenTelegramAnswersNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	err := c.SendMessage(context.Background(), 4242, "hello")
	if err == nil {
		t.Fatal("SendMessage() = nil, want an error when ok is false")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatal("the bot token leaked into an error message")
	}
}

func TestGetUpdatesDecodesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":7,"message":{"text":"/start abc","chat":{"id":99}}}]}`))
	}))
	defer srv.Close()

	c := newClientWithBase("secret-token", srv.URL)
	updates, err := c.GetUpdates(context.Background(), 0, time.Second)
	if err != nil {
		t.Fatalf("GetUpdates() = %v, want nil", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 {
		t.Fatalf("updates = %+v, want one update with id 7", updates)
	}
}

// A token with a stray control character -- a trailing newline from a
// copy-pasted secrets file is the realistic case -- makes the request URL
// itself invalid. http.NewRequestWithContext's error in that case is a
// url.Error, which embeds the full (rejected) URL and therefore the token;
// it must never reach the caller unwrapped, on this path any more than on
// the "telegram answered not ok" path above.
func TestSendMessageDoesNotLeakTokenWhenTokenBreaksTheURL(t *testing.T) {
	token := "secret-token\nwith-a-control-character"
	c := newClientWithBase(token, "https://api.telegram.org")
	err := c.SendMessage(context.Background(), 4242, "hello")
	if err == nil {
		t.Fatal("SendMessage() = nil, want an error for a token that breaks the URL")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("the bot token leaked into an error message: %v", err)
	}
}
