package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Household access list: docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.

type accessBody struct {
	TelegramEnabled bool `json:"telegramEnabled"`
	Tokens          []struct {
		ID         string     `json:"id"`
		MemberID   string     `json:"memberId"`
		MemberName string     `json:"memberName"`
		Name       string     `json:"name"`
		Prefix     string     `json:"prefix"`
		CreatedAt  time.Time  `json:"createdAt"`
		ExpiresAt  time.Time  `json:"expiresAt"`
		LastUsedAt *time.Time `json:"lastUsedAt"`
	} `json:"tokens"`
	Chats []struct {
		MemberID     string    `json:"memberId"`
		MemberName   string    `json:"memberName"`
		ChatUsername string    `json:"chatUsername"`
		LinkedAt     time.Time `json:"linkedAt"`
	} `json:"chats"`
}

const accessURL = "/api/v1/household/access"

// withBot rebuilds the router with a non-nil TelegramLink: the test env has
// no bot, and "no bot configured" is exactly what hides the chats. Only the
// nil check is exercised, so the service's own deps can stay empty.
func (env *testEnv) withBot() {
	d := env.deps
	d.TelegramLink = usecase.NewTelegramLinkService(usecase.TelegramLinkDeps{})
	env.deps = d
	env.router = httpadapter.NewRouter(d)
}

func (env *testEnv) userIDFor(t *testing.T, email string) string {
	t.Helper()
	u, err := env.users.ByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	return u.ID
}

func (env *testEnv) bindChat(t *testing.T, email string, chatID int64, username string) {
	t.Helper()
	if err := env.telegramAccounts.Create(context.Background(), usecase.TelegramBinding{
		UserID: env.userIDFor(t, email), ChatID: chatID, ChatUsername: username,
	}); err != nil {
		t.Fatalf("bind chat for %s: %v", email, err)
	}
}

func (env *testEnv) access(t *testing.T, session *http.Cookie) (accessBody, string) {
	t.Helper()
	rec := env.authedGet(t, accessURL, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET access: %d %s", rec.Code, rec.Body.String())
	}
	var body accessBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode access: %v %s", err, rec.Body.String())
	}
	return body, rec.Body.String()
}

func TestAnOwnerSeesEveryMembersTokensAndChats(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	env.mustCreateToken(t, ownerSession, ownerCSRF, "owner-laptop")
	env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")
	env.bindChat(t, env.limitedEmail, 7001, "ethan_tg")

	body, _ := env.access(t, ownerSession)

	if !body.TelegramEnabled {
		t.Fatal("telegramEnabled = false with a bot configured and the flag on")
	}
	names := map[string]string{}
	for _, tok := range body.Tokens {
		names[tok.Name] = tok.MemberName
	}
	if names["owner-laptop"] == "" || names["ethan-script"] != "Ethan" {
		t.Fatalf("tokens = %+v, want both members' tokens labelled", body.Tokens)
	}
	if len(body.Chats) != 1 || body.Chats[0].MemberName != "Ethan" || body.Chats[0].ChatUsername != "ethan_tg" {
		t.Fatalf("chats = %+v, want Ethan's chat", body.Chats)
	}
}

// PRD 15. The owner has a token and a chat too, so a handler that served
// the household view to everyone fails this test rather than passing it by
// accident.
func TestALimitedMemberSeesOnlyTheirOwnTokensAndChat(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	env.mustCreateToken(t, ownerSession, ownerCSRF, "owner-laptop")
	env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	env.bindChat(t, env.limitedEmail, 7001, "ethan_tg")

	body, _ := env.access(t, limitedSession)

	ethan := env.userIDFor(t, env.limitedEmail)
	if len(body.Tokens) != 1 || body.Tokens[0].Name != "ethan-script" || body.Tokens[0].MemberID != ethan {
		t.Fatalf("tokens = %+v, want only Ethan's own", body.Tokens)
	}
	if len(body.Chats) != 1 || body.Chats[0].MemberID != ethan {
		t.Fatalf("chats = %+v, want only Ethan's own", body.Chats)
	}
}

// Decision 5: a leaked token must not be able to map the household's other
// credentials.
func TestTheAccessListRefusesAnAPIToken(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	tok := env.mustCreateToken(t, session, csrf, "claude")

	rec := env.bearer(t, http.MethodGet, accessURL, nil, tok.Token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
		t.Fatalf("GET access with a token: %d %s, want 403 SESSION_REQUIRED", rec.Code, rec.Body.String())
	}
}

func TestTheAccessListNeedsASignIn(t *testing.T) {
	env := newTestEnv(t)
	if rec := env.do(http.MethodGet, accessURL, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET access signed out: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWithTelegramOffTheAccessListHasNoChats(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	if err := env.featureFlags.SetGlobal(context.Background(), string(domain.FlagTelegramSignIn), false, ""); err != nil {
		t.Fatalf("turn telegram off: %v", err)
	}
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body, raw := env.access(t, session)
	if body.TelegramEnabled || len(body.Chats) != 0 || !strings.Contains(raw, `"chats":[]`) {
		t.Fatalf("with the flag off: %s, want telegramEnabled false and chats []", raw)
	}
}

func TestWithNoBotConfiguredTheAccessListHasNoChats(t *testing.T) {
	env := newTestEnv(t) // no withBot(): deps.TelegramLink is nil
	env.bindChat(t, env.ownerEmail, 7000, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	body, _ := env.access(t, session)
	if body.TelegramEnabled || len(body.Chats) != 0 {
		t.Fatalf("with no bot: %+v, want telegramEnabled false and no chats", body)
	}
}

// Decision 9: the chat id is a Telegram identifier the list does not need.
func TestTheAccessListNeverSendsAChatID(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	env.bindChat(t, env.ownerEmail, 987654321, "owner_tg")
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	_, raw := env.access(t, session)
	if strings.Contains(raw, "987654321") || strings.Contains(strings.ToLower(raw), "chatid") {
		t.Fatalf("access body leaks the chat id: %s", raw)
	}
}

func TestAnEmptyAccessListAnswersEmptyListsNotNull(t *testing.T) {
	env := newTestEnv(t)
	env.withBot()
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	_, raw := env.access(t, session)
	if !strings.Contains(raw, `"tokens":[]`) || !strings.Contains(raw, `"chats":[]`) {
		t.Fatalf("empty access body = %s, want [] for both lists", raw)
	}
}

// Decision 6: seeing a partner's token is not the power to revoke it. The
// existing DELETE is scoped to the token's owner, and this pins it.
func TestAnOwnerCannotRevokeAnotherMembersToken(t *testing.T) {
	env := newTestEnv(t)
	ownerSession, ownerCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	limitedSession, limitedCSRF := env.signIn(t, env.limitedEmail, env.limitedPassword)
	ethans := env.mustCreateToken(t, limitedSession, limitedCSRF, "ethan-script")

	rec := env.authed(t, http.MethodDelete, "/api/v1/auth/tokens/"+ethans.ID, nil, ownerSession, ownerCSRF)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("owner revoking Ethan's token: %d %s, want 404", rec.Code, rec.Body.String())
	}
	if rec := env.bearer(t, http.MethodGet, "/api/v1/auth/me", nil, ethans.Token); rec.Code != http.StatusOK {
		t.Fatalf("Ethan's token after the refused revoke: %d, want it still working", rec.Code)
	}
}
