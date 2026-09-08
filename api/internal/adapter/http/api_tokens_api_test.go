package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Personal API tokens (docs/superpowers/specs/2026-09-08-hearth-api-tokens-design.md).

type createdTokenBody struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Token  string `json:"token"`
}

func (env *testEnv) mustCreateToken(t *testing.T, session, csrf *http.Cookie, name string) createdTokenBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"name": name}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", rec.Code, rec.Body.String())
	}
	var body createdTokenBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("create token body: %v %s", err, rec.Body.String())
	}
	return body
}

// bearer issues a request with only an Authorization header: no cookies, no
// CSRF header -- exactly what hearthctl sends once it holds a token.
func (env *testEnv) bearer(t *testing.T, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestATokenReachesAMoneyRouteAndWritesWithoutCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "claude")

	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); rec.Code != http.StatusOK {
		t.Fatalf("whoami with a token: %d %s", rec.Code, rec.Body.String())
	}
	rec := env.bearer(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"nickname": "Token account", "type": "cash", "openingBalanceMinor": 100,
		"openingBalanceCurrency": "SGD", "openingBalanceAsOf": "2026-01-01",
	}, tok.Token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a token write must not need the CSRF header: %d %s", rec.Code, rec.Body.String())
	}
}

func TestATokenCarriesTheMembersOwnCapabilities(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
	tok := env.mustCreateToken(t, session, csrf, "kid")
	rec := env.bearer(t, http.MethodGet, "/api/v1/accounts", nil, tok.Token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a limited member's token must get the limited member's 403, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestABadTokenBesideAGoodCookieIsStill401(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)
	for _, header := range []string{"Bearer hearth_nope", "Bearer notaprefix", "Basic abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.AddCookie(session)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization %q with a valid cookie must not fall back to the cookie: got %d", header, rec.Code)
		}
	}
}

func TestRevokedAndExpiredTokensAre401(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "soon gone")

	if rec := env.authed(t, http.MethodDelete, "/api/v1/auth/tokens/"+tok.ID, nil, session, csrf); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", rec.Code)
	}
	if rec := env.authed(t, http.MethodDelete, "/api/v1/auth/tokens/"+tok.ID, nil, session, csrf); rec.Code != http.StatusNotFound {
		t.Fatalf("revoking twice must be 404, got %d", rec.Code)
	}

	// Expired: written straight through the repository, because the
	// service refuses to mint one that is already dead.
	raw := "hearth_expiredexpiredexpired"
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	json.Unmarshal(env.authedGet(t, "/api/v1/auth/me", session).Body.Bytes(), &me)
	_, err := env.apiTokens.Create(context.Background(), env.tokens.HashToken(raw), "expired", domain.APIToken{
		UserID: me.User.ID, HouseholdID: env.householdID, Name: "old", ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, raw); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: %d", rec.Code)
	}
}

func TestATokenCannotMintOrRevokeTokensOrSignOut(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "first")

	if rec := env.bearer(t, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"name": "second"}, tok.Token); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
		t.Fatalf("a token minting a token: %d %s", rec.Code, rec.Body.String())
	}
	if rec := env.bearer(t, http.MethodDelete, "/api/v1/auth/tokens/"+tok.ID, nil, tok.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("a token revoking a token: %d", rec.Code)
	}
	if rec := env.bearer(t, http.MethodPost, "/api/v1/auth/sign-out", nil, tok.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("a token signing out: %d", rec.Code)
	}
	// Listing is fine: it shows no secrets.
	rec := env.bearer(t, http.MethodGet, "/api/v1/auth/tokens", nil, tok.Token)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), tok.Token) {
		t.Fatalf("list: %d, or it leaked the raw token: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), tok.Prefix) {
		t.Fatalf("list should carry the prefix so tokens can be told apart: %s", rec.Body.String())
	}
}

func TestATokenCannotReachAdmin(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "x")
	if rec := env.bearer(t, http.MethodGet, "/api/v1/admin/flags", nil, tok.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("a token on /admin must get the non-admin 404, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRemovingAMemberKillsTheirToken(t *testing.T) {
	env := newTestEnv(t)
	owner, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limited, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	tok := env.mustCreateToken(t, limited, limitedCSRF, "kid")
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); rec.Code != http.StatusOK {
		t.Fatalf("token should work before removal: %d", rec.Code)
	}
	if rec := env.authed(t, http.MethodDelete, "/api/v1/household/members/"+env.limitedMembership, nil, owner, ownerCSRF); rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: %d %s", rec.Code, rec.Body.String())
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, tok.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a removed member's token must be dead, got %d", rec.Code)
	}
}

func TestCreateTokenRefusesABadNameOrLifetime(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	if rec := env.authed(t, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"name": " "}, session, csrf); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank name: %d", rec.Code)
	}
	if rec := env.authed(t, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"name": "x", "expiresInDays": 400}, session, csrf); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("400 days: %d", rec.Code)
	}
}
