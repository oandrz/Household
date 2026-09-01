package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to Telegram's Bot API. It holds the bot token and must never
// put it in an error, a log line, or anything else that leaves this file --
// the token is a full credential for the bot, and Telegram's own API URLs
// embed it in the path, which is exactly why errors here are built from the
// method name rather than from the request URL.
type Client struct {
	token string
	base  string
	http  *http.Client
}

// NewClient uses Telegram's public API host. The send timeout is generous
// because nothing waits on a send: the poller is not on any request path.
func NewClient(token string) *Client {
	return newClientWithBase(token, "https://api.telegram.org")
}

// newClientWithBase exists so tests can point the client at an httptest server
// without reaching Telegram. It is unexported: production has one host.
func newClientWithBase(token, base string) *Client {
	return &Client{token: token, base: base, http: &http.Client{Timeout: 70 * time.Second}}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

// call posts a JSON body to one Bot API method and refuses anything that is
// not ok. Telegram answers 200 with ok:false for application-level failures,
// so a status-code check alone would read a refused send as a success.
func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	url := fmt.Sprintf("%s/bot%s/%s", c.base, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Deliberately not %w on err alone: url.Error carries the request URL,
		// and the URL contains the bot token.
		return fmt.Errorf("telegram %s: request failed", method)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if !parsed.OK {
		return fmt.Errorf("telegram %s refused: %s", method, parsed.Description)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(parsed.Result, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// SendMessage satisfies usecase.TelegramSender. Messages are plain text by
// design -- there is no template system to keep in sync with the product copy,
// exactly as the mailer has none.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}, nil)
}

// GetUpdates long-polls. timeout is Telegram's own server-side wait, which is
// why the HTTP client's timeout above is comfortably longer than any value a
// caller will pass.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":  offset,
		"timeout": int(timeout.Seconds()),
	}, &updates)
	if err != nil {
		return nil, err
	}
	return updates, nil
}
