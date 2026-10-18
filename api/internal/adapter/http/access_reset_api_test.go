package httpadapter_test

import (
	"context"
	"net/http"
	"testing"
)

const (
	passwordURL          = "/api/v1/auth/password"
	signOutEverywhereURL = "/api/v1/auth/sign-out-everywhere"
)

func cookieNamed(rec interface{ Result() *http.Response }, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestChangingThePasswordSwapsThisBrowsersSessionAndEndsEveryOther(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	otherBrowser, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "script")

	rec := env.authed(t, http.MethodPost, passwordURL,
		map[string]string{"currentPassword": env.ownerPassword, "newPassword": "a-brand-new-password"}, session, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password = %d %s", rec.Code, rec.Body.String())
	}
	fresh := cookieNamed(rec, "hearth_session")
	if fresh == nil || fresh.Value == session.Value {
		t.Fatalf("no fresh session cookie in %v", rec.Result().Cookies())
	}

	if got := env.authedGet(t, "/api/v1/auth/me", fresh); got.Code != http.StatusOK {
		t.Fatalf("the fresh session = %d, want 200", got.Code)
	}
	for name, c := range map[string]*http.Cookie{"this browser's old session": session, "the other browser": otherBrowser} {
		if got := env.authedGet(t, "/api/v1/auth/me", c); got.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", name, got.Code)
		}
	}
	if got := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); got.Code != http.StatusUnauthorized {
		t.Errorf("the API token = %d, want 401", got.Code)
	}
	env.signIn(t, env.ownerEmail, "a-brand-new-password")
}

// The frontend signs the browser out on any 401 outside the sign-in pages,
// so a typo in Settings answered with 401 would sign the person out.
func TestAWrongCurrentPasswordIs403NotA401ThatWouldSignTheBrowserOut(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	rec := env.authed(t, http.MethodPost, passwordURL,
		map[string]string{"currentPassword": "not-it-at-all", "newPassword": "a-brand-new-password"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusForbidden, "CURRENT_PASSWORD_WRONG")
	if got := env.authedGet(t, "/api/v1/auth/me", session); got.Code != http.StatusOK {
		t.Fatalf("still signed in after a typo? /auth/me = %d", got.Code)
	}
}

func TestThreeWrongCurrentPasswordsLockThePasswordCheck(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	body := map[string]string{"currentPassword": "not-it-at-all", "newPassword": "a-brand-new-password"}
	for i := 0; i < 3; i++ {
		env.authed(t, http.MethodPost, passwordURL, body, session, csrf)
	}
	body["currentPassword"] = env.ownerPassword
	assertErrorResponse(t, env.authed(t, http.MethodPost, passwordURL, body, session, csrf),
		http.StatusLocked, "PASSWORD_CHECKS_LOCKED")
}

func TestSignOutEverywhereEndsEverySessionTokenAndTheChat(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	otherBrowser, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "script")
	env.bindChat(t, env.ownerEmail, 5551, "andreas_o")

	rec := env.authed(t, http.MethodPost, signOutEverywhereURL, nil, session, csrf)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign out everywhere = %d %s", rec.Code, rec.Body.String())
	}
	if c := cookieNamed(rec, "hearth_session"); c == nil || c.MaxAge >= 0 {
		t.Errorf("session cookie not cleared: %+v", c)
	}
	for name, c := range map[string]*http.Cookie{"this browser": session, "the other browser": otherBrowser} {
		if got := env.authedGet(t, "/api/v1/auth/me", c); got.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", name, got.Code)
		}
	}
	if got := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); got.Code != http.StatusUnauthorized {
		t.Errorf("the API token = %d, want 401", got.Code)
	}
	if _, err := env.telegramAccounts.ByUserID(context.Background(), env.userIDFor(t, env.ownerEmail)); err == nil {
		t.Error("the Telegram chat is still linked")
	}
}

func TestATokenCannotChangeThePasswordOrSignOutEverywhere(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "script")
	for _, path := range []string{passwordURL, signOutEverywhereURL} {
		rec := env.bearer(t, http.MethodPost, path,
			map[string]string{"currentPassword": env.ownerPassword, "newPassword": "a-brand-new-password"}, tok.Token)
		assertErrorResponse(t, rec, http.StatusForbidden, "SESSION_REQUIRED")
	}
}
