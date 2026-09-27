package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// lockHoldingMembers is the real MembershipRepository with one change: while
// UpdateWithCheck holds the household lock, it signals holdingLock and then
// keeps the lock for 300ms before running the caller's decide. That is how the
// test below makes a second request arrive while the first is mid-write,
// deterministically rather than by scheduling luck (docs/LEARNING.md
// pattern 19). It is meant for exactly one update.
type lockHoldingMembers struct {
	usecase.MembershipRepository
	holdingLock chan struct{}
}

func (m lockHoldingMembers) UpdateWithCheck(ctx context.Context, householdID, membershipID string,
	decide func([]domain.Membership) (domain.Role, domain.Capabilities, error)) error {
	return m.MembershipRepository.UpdateWithCheck(ctx, householdID, membershipID,
		func(current []domain.Membership) (domain.Role, domain.Capabilities, error) {
			close(m.holdingLock)
			time.Sleep(300 * time.Millisecond)
			return decide(current)
		})
}

// A PATCH that leaves a field out means "keep what is there". The question
// is WHEN "what is there" is read. If it is read before the household lock,
// a capabilities-only PATCH can carry a stale role into the write and
// silently undo a role change that committed in between.
//
// Here the owner promotes the limited member to owner and, while that
// promotion holds the lock, sends a capabilities-only PATCH for the same
// member. The capabilities-only request must see the promotion. It must
// not write the old "limited" role back. The last-owner rule does not catch
// this, because the caller is still an owner.
func TestACapabilitiesOnlyPatchCannotUndoARoleChangeItRacedWith(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	// The promotion goes through its own router, whose MemberService holds
	// the lock open. Everything else is the env's real wiring.
	holdingLock := make(chan struct{})
	slowDeps := env.deps
	slowDeps.Members = usecase.NewMemberService(usecase.MemberDeps{
		Members:   lockHoldingMembers{MembershipRepository: postgres.NewMembershipRepo(env.db), holdingLock: holdingLock},
		Sessions:  postgres.NewSessionRepo(env.db),
		APITokens: postgres.NewAPITokenRepo(env.db),
	})
	slowRouter := httpadapter.NewRouter(slowDeps)

	path := "/api/v1/household/members/" + env.limitedMembership
	promoted := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		promoted <- serveAuthed(t, slowRouter, http.MethodPatch, path,
			map[string]any{"role": "owner", "capabilities": []string{"calendar", "chores", "money", "marriage"}},
			session, csrf)
	}()

	// Only once the promotion holds the lock: the capabilities-only PATCH.
	<-holdingLock
	capsOnly := env.authed(t, http.MethodPatch, path,
		map[string]any{"capabilities": []string{"calendar"}}, session, csrf)
	promotion := <-promoted

	if promotion.Code != http.StatusOK {
		t.Fatalf("promotion status = %d, body = %s", promotion.Code, promotion.Body.String())
	}
	persisted := mustFindMember(t, env.getMembers(t, session), env.limitedMembership)
	if persisted.Role != "owner" {
		t.Fatalf("member is %q after the race, want owner -- the capabilities-only PATCH (status %d, body %s) "+
			"wrote a stale role over the promotion", persisted.Role, capsOnly.Code, capsOnly.Body.String())
	}
	// Seen after the promotion, "only calendar" is not a valid set for an
	// owner, so the capabilities-only PATCH is refused rather than applied.
	assertErrorResponse(t, capsOnly, http.StatusUnprocessableEntity, "INVALID_CAPABILITIES")
}

// serveAuthed is env.authed against a router of the test's choosing.
func serveAuthed(t *testing.T, router http.Handler, method, path string, body any, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Errorf("marshal body: %v", err)
		return httptest.NewRecorder()
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
