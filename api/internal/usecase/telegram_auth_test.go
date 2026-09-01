package usecase_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func TestStartLinkMintsADeepLinkAndStoresTheNonceHashed(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)

	link, err := svc.StartLink(context.Background())
	if err != nil {
		t.Fatalf("StartLink() = %v, want nil", err)
	}
	if !strings.HasPrefix(link.URL, "https://t.me/HearthBot?start=") {
		t.Fatalf("URL = %q, want a t.me deep link for HearthBot", link.URL)
	}
	raw := strings.TrimPrefix(link.URL, "https://t.me/HearthBot?start=")
	if doubles.links.hasRaw(raw) {
		t.Fatal("the raw nonce was stored; it must be stored hashed")
	}
	if !doubles.links.hasHashOf(raw) {
		t.Fatal("no row was stored for the minted nonce")
	}
	// 10 minutes mirrors telegram_auth.go's unexported telegramNonceTTL,
	// hardcoded here because this file cannot see an unexported constant.
	// doubles.clock is a fixedClock that never advances on its own, so this
	// is an exact equality, not a tolerance check -- a service that returned
	// a zero-valued or otherwise wrong ExpiresAt while still passing the
	// right value to Links.Create would pass every other assertion in this
	// test and still ship a broken expiry to whatever calls StartLink.
	if want := doubles.clock.Now().Add(10 * time.Minute); !link.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", link.ExpiresAt, want)
	}
}

func TestHandleStartSendsASignInLinkToAKnownChat(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(501, "user-1")
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 501, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	sent := doubles.sender.lastTo(501)
	if !strings.Contains(sent, "/sign-in/magic?token=") {
		t.Fatalf("message = %q, want it to carry a magic-link URL", sent)
	}
	if doubles.magicLinks.countFor("user-1") != 1 {
		t.Fatalf("magic links minted = %d, want 1", doubles.magicLinks.countFor("user-1"))
	}
}

func TestHandleStartSendsASignUpLinkToAnUnknownChat(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 777, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	sent := doubles.sender.lastTo(777)
	if !strings.Contains(sent, "/sign-up/") {
		t.Fatalf("message = %q, want it to carry a sign-up URL", sent)
	}
	if doubles.signups.telegramCount(777) != 1 {
		t.Fatalf("telegram signups created = %d, want 1", doubles.signups.telegramCount(777))
	}
}

// An unknown nonce, an expired one and an already-consumed one must all answer
// identically, so none of them can be told apart by probing.
func TestHandleStartAnswersIdenticallyForEveryDeadNonce(t *testing.T) {
	unknown := func(d *telegramDoubles) string { return "never-minted" }
	expired := func(d *telegramDoubles) string { return d.links.mintLive(nil, time.Now().Add(-time.Minute)) }
	consumed := func(d *telegramDoubles) string {
		raw := d.links.mintLive(nil, time.Now().Add(10*time.Minute))
		d.links.markConsumed(raw, 900)
		return raw
	}

	var answers []string
	for _, mint := range []func(*telegramDoubles) string{unknown, expired, consumed} {
		svc, doubles := newTelegramAuthService(t)
		raw := mint(doubles)
		if err := svc.HandleStart(context.Background(), 900, raw); err != nil {
			t.Fatalf("HandleStart() = %v, want nil", err)
		}
		answers = append(answers, doubles.sender.lastTo(900))
	}
	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Fatalf("dead-nonce answers differ: %q", answers)
	}
	if strings.Contains(answers[0], "/sign-in/") || strings.Contains(answers[0], "/sign-up/") {
		t.Fatalf("a dead nonce leaked a link: %q", answers[0])
	}
}

// Over the per-chat limit answers with the same message a dead nonce gets, so
// being rate-limited is not distinguishable from being late.
func TestHandleStartRateLimitsPerChatWithTheSameAnswer(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(600, "user-2")
	doubles.links.recordRedemptions(600, 3, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 600, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.magicLinks.countFor("user-2") != 0 {
		t.Fatal("a magic link was minted for a rate-limited chat")
	}
	if got := doubles.sender.lastTo(600); !strings.Contains(got, "expired") {
		t.Fatalf("message = %q, want the same expiry answer a dead nonce gets", got)
	}
}

// The nonce is spent even when the chat is over its limit, so the same link
// cannot be retried until the hour rolls over.
func TestHandleStartSpendsTheNonceEvenWhenRateLimited(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.links.recordRedemptions(601, 3, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	_ = svc.HandleStart(context.Background(), 601, raw)
	if !doubles.links.isConsumed(raw) {
		t.Fatal("a rate-limited attempt left its nonce unspent")
	}
}

// CountLinksSince's real SQL boundary is `consumed_at >= since` (inclusive --
// see queries/telegram.sql), which Task 3's repository tests never covered
// (see this task's brief). A redemption landing exactly on the hour-old
// cutoff must still count toward the limit; if the boundary were exclusive
// instead, this exact case would let a chat squeeze out one extra redemption
// right at the edge of the window every single hour.
func TestHandleStartRateLimitCountsARedemptionExactlyOnTheSinceBoundary(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	cutoff := doubles.clock.Now().Add(-time.Hour)
	doubles.links.recordRedemptions(700, 3, cutoff)
	raw := doubles.links.mintLive(t, doubles.clock.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 700, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(700); !strings.Contains(got, "expired") {
		t.Fatalf("message = %q, want the rate-limit answer once the boundary redemption is counted", got)
	}
}

// R5: SignupService.Request's global daily ceiling (SignupGlobalDailyLimit)
// covers rows in the signups table with no channel filter, and
// CreateForTelegram writes into that same table. Without a check here, a
// flood of Telegram sign-ups could silently exhaust the ceiling meant for
// email sign-up while Telegram sign-up itself stayed unbounded. The ceiling
// answers with the identical telegramDeadLinkMessage every other refusal
// uses -- pinned here by comparing against a second fixture's unknown-nonce
// answer, not by matching a substring, so this test cannot pass against an
// implementation that merely refuses with *some* message.
func TestHandleStartRefusesSignUpAtTheGlobalDailyCeilingWithTheSameAnswerAsADeadNonce(t *testing.T) {
	baseline, baseDoubles := newTelegramAuthService(t)
	if err := baseline.HandleStart(context.Background(), 950, "never-minted"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	deadNonceAnswer := baseDoubles.sender.lastTo(950)

	svc, doubles := newTelegramAuthService(t)
	// setGlobalCount is set to exactly the limit, not one above it, to pin
	// that the check is >=, matching SignupService.Request's own >= check --
	// a > check would let exactly one Telegram sign-up through right at the
	// ceiling every day.
	doubles.signups.setGlobalCount(usecase.SignupGlobalDailyLimit)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 888, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.signups.telegramCount(888) != 0 {
		t.Fatal("a signup row was created over the global daily ceiling")
	}
	got := doubles.sender.lastTo(888)
	if got != deadNonceAnswer {
		t.Fatalf("ceiling answer = %q, want the identical dead-nonce answer %q", got, deadNonceAnswer)
	}
}

// Below the global ceiling, a fresh chat's sign-up is unaffected by it -- the
// check must not refuse everyone just because Signups.CountSince was called.
func TestHandleStartSendsASignUpLinkBelowTheGlobalDailyCeiling(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.signups.setGlobalCount(usecase.SignupGlobalDailyLimit - 1)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 889, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.signups.telegramCount(889) != 1 {
		t.Fatalf("telegram signups created = %d, want 1", doubles.signups.telegramCount(889))
	}
}
