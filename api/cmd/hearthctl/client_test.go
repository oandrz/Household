package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAPI records what arrived so a test can assert on the cookies and
// headers hearthctl sent, without the real router (which would couple the
// client binary to server internals -- the thing the CLI exists to avoid).
type fakeAPI struct {
	t         *testing.T
	seen      []*http.Request
	bodies    [][]byte
	respond   func(w http.ResponseWriter, r *http.Request)
	sessionOK string
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{t: t, sessionOK: "sess-1"}
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.seen = append(f.seen, r)
		f.bodies = append(f.bodies, raw)
		f.respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

// signIn is what the real handler does: two cookies and the me bundle.
func signInResponder(session, csrf string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/sign-in" {
			http.Error(w, `{"error":{"code":"UNAUTHENTICATED"}}`, 401)
			return
		}
		exp := time.Now().Add(30 * 24 * time.Hour)
		http.SetCookie(w, &http.Cookie{Name: "hearth_session", Value: session, Path: "/", HttpOnly: true, Expires: exp})
		http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: csrf, Path: "/", Expires: exp})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":{"id":"u1"},"membership":{"id":"m1"}}`))
	}
}

func run_(t *testing.T, srvURL string, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	full := append([]string{"--url", srvURL}, args...)
	err = run(context.Background(), full, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), err
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("not an exitError: %v", err)
	}
	return ee.code
}

func TestLoginStoresTheSessionAndPrintsTheMeBundle(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	t.Setenv("HEARTH_PASSWORD", "")
	f, srv := newFakeAPI(t)
	f.respond = signInResponder("sess-1", "csrf-1")

	out, _, err := run_(t, srv.URL, "hunter2\n", "login", "--email=a@example.com")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(out, `"membership":{"id":"m1"}`) {
		t.Fatalf("stdout should carry the me bundle, got %q", out)
	}
	var sent map[string]string
	if err := json.Unmarshal(f.bodies[0], &sent); err != nil || sent["email"] != "a@example.com" || sent["password"] != "hunter2" {
		t.Fatalf("sign-in body wrong: %v %v", sent, err)
	}

	st, _ := newStore(srv.URL)
	creds, err := st.load()
	if err != nil {
		t.Fatalf("credentials not stored: %v", err)
	}
	if creds.Session != "sess-1" || creds.CSRF != "csrf-1" || creds.Email != "a@example.com" {
		t.Fatalf("stored %+v", creds)
	}
}

func TestReadsCarryTheSessionButNotTheCSRFHeader(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	if err := st.save(&credentials{BaseURL: srv.URL, Session: "sess-1", CSRF: "csrf-1"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run_(t, srv.URL, "", "list", "accounts"); err != nil {
		t.Fatal(err)
	}
	r := f.seen[0]
	if r.Method != "GET" || r.URL.Path != "/api/v1/accounts" {
		t.Fatalf("wrong request %s %s", r.Method, r.URL.Path)
	}
	if c, err := r.Cookie("hearth_session"); err != nil || c.Value != "sess-1" {
		t.Fatalf("session cookie missing on a read")
	}
	if r.Header.Get("X-CSRF-Token") != "" {
		t.Fatalf("a read must not carry the CSRF header")
	}
}

func TestWritesCarryTheCSRFCookieAndMatchingHeader(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "sess-1", CSRF: "csrf-1"})

	_, _, err := run_(t, srv.URL, "", "transaction", "add",
		"--kind=expense", "--date=2026-09-08", "--description=Coffee",
		"--amount-minor=650", "--from-account=acc-1", "--category=cat-1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.seen[0]
	if r.Method != "POST" || r.URL.Path != "/api/v1/transactions" {
		t.Fatalf("wrong request %s %s", r.Method, r.URL.Path)
	}
	c, err := r.Cookie("csrf_token")
	if err != nil || c.Value != "csrf-1" || r.Header.Get("X-CSRF-Token") != "csrf-1" {
		t.Fatalf("double-submit pair missing: cookie=%v header=%q", c, r.Header.Get("X-CSRF-Token"))
	}
	if r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", r.Header.Get("Content-Type"))
	}
}

func TestA401MeansSignInAgainAndNeverRetries(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "stale", CSRF: "x"})
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"UNAUTHENTICATED"}}`, 401)
	}

	_, _, err := run_(t, srv.URL, "", "whoami")
	if code := exitCode(t, err); code != exitSignInAgain {
		t.Fatalf("exit %d, want %d", code, exitSignInAgain)
	}
	if len(f.seen) != 1 {
		t.Fatalf("made %d requests; a 401 must never be retried", len(f.seen))
	}
	if !strings.Contains(err.Error(), "hearthctl login") {
		t.Fatalf("message should say how to recover: %q", err.Error())
	}
}

func TestAWrongPasswordShowsAttemptsRemainingAndNeverSaysRetry(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	t.Setenv("HEARTH_PASSWORD", "wrong")
	f, srv := newFakeAPI(t)
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"INVALID_CREDENTIALS","details":{"attemptsRemaining":2}}}`, 401)
	}

	out, _, err := run_(t, srv.URL, "", "login", "--email=a@example.com")
	if code := exitCode(t, err); code != exitSignInAgain {
		t.Fatalf("exit %d, want %d", code, exitSignInAgain)
	}
	if !strings.Contains(out, `"attemptsRemaining":2`) {
		t.Fatalf("the attempts-remaining body must reach stdout, got %q", out)
	}
	if strings.Contains(err.Error(), "hearthctl login") {
		t.Fatalf("a refused sign-in must not tell the caller to run login again: %q", err.Error())
	}
	if len(f.seen) != 1 {
		t.Fatalf("made %d requests; a refused sign-in must never be retried", len(f.seen))
	}
	st, _ := newStore(srv.URL)
	if _, err := st.load(); err == nil {
		t.Fatalf("no credentials may be stored after a refused sign-in")
	}
}

func TestOtherErrorsPrintTheBodyAndExit3(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"VALIDATION","message":"nope"}}`, 422)
	}

	out, _, err := run_(t, srv.URL, "", "category", "add", "--name=Food")
	if code := exitCode(t, err); code != exitAPIRefused {
		t.Fatalf("exit %d, want %d", code, exitAPIRefused)
	}
	if !strings.Contains(out, `"VALIDATION"`) {
		t.Fatalf("the API's own error body must reach stdout, got %q", out)
	}
}

func TestA204PrintsNothing(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	f.respond = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }

	out, _, err := run_(t, srv.URL, "", "api", "DELETE", "/transactions/t1")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("stdout should be empty for a 204, got %q", out)
	}
}

func TestWithoutACredentialFileAnAuthenticatedCommandExits2WithoutARequest(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	_, _, err := run_(t, srv.URL, "", "list", "bills")
	if code := exitCode(t, err); code != exitSignInAgain {
		t.Fatalf("exit %d, want %d", code, exitSignInAgain)
	}
	if len(f.seen) != 0 {
		t.Fatalf("should not have called the server")
	}
}

func TestUnreachableServerExits4(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	_, _, err := run_(t, "http://127.0.0.1:1", "", "api", "GET", "/currencies")
	if code := exitCode(t, err); code != exitUnreachable {
		t.Fatalf("exit %d, want %d", code, exitUnreachable)
	}
}

func TestLogoutForgetsTheFileEvenIfTheServerRefuses(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"UNAUTHENTICATED"}}`, 401)
	}
	_, _, err := run_(t, srv.URL, "", "logout")
	if exitCode(t, err) != exitSignInAgain {
		t.Fatalf("expected the 401 to surface, got %v", err)
	}
	if _, err := st.load(); err == nil {
		t.Fatalf("credential file should be gone after logout")
	}
}

func TestAPIPassesTheBodyThroughUntouched(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	_, _, err := run_(t, srv.URL, "", "api", "PUT", "/budgets/2026-09", `--data={"lines":[{"categoryId":"c1","capMinor":100}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.bodies[0]); got != `{"lines":[{"categoryId":"c1","capMinor":100}]}` {
		t.Fatalf("body altered: %s", got)
	}
	if f.seen[0].Method != "PUT" {
		t.Fatalf("method %s", f.seen[0].Method)
	}
}
