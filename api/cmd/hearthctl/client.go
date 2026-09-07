package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// These two names are the server's, copied from
// internal/adapter/http/middleware_session.go and middleware_csrf.go. The
// CLI stores the cookie values itself rather than using net/http's cookie
// jar: a jar hides expiry and cannot be written to a file with a
// permission mode, and there are only ever two cookies to track.
const (
	sessionCookieName = "hearth_session"
	csrfCookieName    = "csrf_token"
	csrfHeaderName    = "X-CSRF-Token"
	apiPrefix         = "/api/v1"
)

// client talks to one Hearth API. It carries the credentials the store
// loaded, and updates them from Set-Cookie on sign-in and sign-out.
type client struct {
	baseURL string
	http    *http.Client
	store   *store
	creds   *credentials // nil until login, or after logout
}

func newClient(baseURL string, st *store) *client {
	c := &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
		store:   st,
	}
	// A missing or unreadable file simply means "not signed in"; the first
	// authenticated call will get a 401 and say so.
	if creds, err := st.load(); err == nil {
		c.creds = creds
	}
	return c
}

// response is what every command sees: the status and the raw body. The
// body is passed through to stdout untouched, so the CLI never reshapes
// what the API said.
type response struct {
	status int
	body   []byte
}

// do sends one request. path is relative to /api/v1 unless it already
// starts with /api/. The session cookie goes on every call; the CSRF cookie
// and its matching header go on writes only, mirroring exactly what
// requireCSRF checks so a read never carries a token it does not need.
//
// It returns an *exitError only for a network failure, and a plain response
// for every status -- a 401 included, because its body matters: a wrong
// password's 401 carries "attemptsRemaining", the one number that tells a
// caller to stop before the household locks. refuse below maps it.
func (c *client) do(ctx context.Context, method, path string, body []byte) (response, error) {
	if !strings.HasPrefix(path, "/api/") {
		path = apiPrefix + "/" + strings.TrimLeft(path, "/")
	}
	if _, err := url.Parse(c.baseURL + path); err != nil {
		return response{}, fail(exitUsage, "bad URL %q: %v", c.baseURL+path, err)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return response{}, fail(exitUsage, "%v", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.creds != nil {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: c.creds.Session})
		if isWrite(method) && c.creds.CSRF != "" {
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: c.creds.CSRF})
			req.Header.Set(csrfHeaderName, c.creds.CSRF)
		}
	}

	res, err := c.http.Do(req)
	if err != nil {
		return response{}, fail(exitUnreachable, "cannot reach %s: %v", c.baseURL, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, fail(exitUnreachable, "reading response from %s: %v", c.baseURL, err)
	}

	c.absorbCookies(res.Cookies())
	return response{status: res.StatusCode, body: raw}, nil
}

func isWrite(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// absorbCookies records the session and CSRF cookies a sign-in sets, and
// forgets them when sign-out clears them (an empty value or a past expiry).
// Any other cookie is ignored: the CLI is not a browser and has no business
// keeping state the API did not ask it to.
func (c *client) absorbCookies(cookies []*http.Cookie) {
	for _, ck := range cookies {
		switch ck.Name {
		case sessionCookieName, csrfCookieName:
		default:
			continue
		}
		if c.creds == nil {
			c.creds = &credentials{BaseURL: c.baseURL}
		}
		cleared := ck.Value == "" || ck.MaxAge < 0 || (!ck.Expires.IsZero() && ck.Expires.Before(time.Now()))
		switch ck.Name {
		case sessionCookieName:
			c.creds.Session = ck.Value
			if !cleared && !ck.Expires.IsZero() {
				c.creds.ExpiresAt = ck.Expires
			}
		case csrfCookieName:
			c.creds.CSRF = ck.Value
		}
		if cleared {
			c.creds = nil
			return
		}
	}
}

// refuse turns any non-2xx into an error after the body has been shown. The
// body is the API's own {"error": {...}} envelope, which is more useful to
// the caller than anything the CLI could paraphrase. A 401 is exit 2 with
// the recovery step spelled out; everything else is exit 3.
func refuse(res response, stdout io.Writer) error {
	if res.status >= 200 && res.status < 300 {
		return nil
	}
	writeBody(stdout, res.body)
	if res.status == http.StatusUnauthorized {
		return fail(exitSignInAgain, "the API answered 401: not signed in, or the session expired. Run: hearthctl login --email=<you>")
	}
	return fail(exitAPIRefused, "the API answered %d", res.status)
}

// refuseSignIn is refuse for the sign-in call itself, where a 401 means a
// wrong password rather than a missing session. The message must not say
// "run login again": after a few more tries the household is locked, and
// an agent that obeys that advice in a loop is how that happens.
func refuseSignIn(res response, stdout io.Writer) error {
	if res.status == http.StatusUnauthorized || res.status == http.StatusLocked {
		writeBody(stdout, res.body)
		return fail(exitSignInAgain, "sign-in refused (%d). Do not retry with the same password: a few more failures lock the whole household. The body above says how many attempts remain.", res.status)
	}
	return refuse(res, stdout)
}

// writeBody prints a body followed by exactly one newline, or nothing at all
// for an empty one (a 204). Tools that pipe hearthctl into jq depend on both.
func writeBody(w io.Writer, body []byte) {
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	w.Write(bytes.TrimRight(body, "\n"))
	io.WriteString(w, "\n")
}

var errNotSignedIn = errors.New("not signed in")

// requireCreds is the client-side short-circuit for commands that cannot
// mean anything without a session. It saves a round trip, but it is not the
// guard: the server's 401 is, and do maps that to the same exit code.
func (c *client) requireCreds() error {
	if c.creds == nil || c.creds.Session == "" {
		return fail(exitSignInAgain, "%v to %s. Run: hearthctl login --email=<you>", errNotSignedIn, c.baseURL)
	}
	return nil
}

func (c *client) String() string { return fmt.Sprintf("hearth@%s", c.baseURL) }
