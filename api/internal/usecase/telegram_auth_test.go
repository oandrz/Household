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

// deadNonceAnswer computes the dead-link answer via a fresh fixture and an
// unknown nonce, so every test in this file that needs to prove "this
// refusal is byte-identical to an ordinary dead nonce" computes its baseline
// the same way, rather than substring-matching a message this package cannot
// see the literal text of (telegramDeadLinkMessage is unexported). A
// substring check like strings.Contains(got, "expired") also passes for a
// message that leaks extra information alongside the expected text -- e.g.
// telegramDeadLinkMessage+" Too many attempts." -- which is exactly the
// enumeration oracle the identical-answer property exists to close. Byte
// equality against an independently-computed baseline is what actually
// pins it.
func deadNonceAnswer(t *testing.T) string {
	t.Helper()
	svc, doubles := newTelegramAuthService(t)
	const probeChatID = int64(1)
	if err := svc.HandleStart(context.Background(), probeChatID, "never-minted"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	return doubles.sender.lastTo(probeChatID)
}

// startOfDayForTest recomputes midnight of t, the way telegram_auth.go's
// unexported startOfDay does (signup.go's own function, reused by
// sendSignUp), since this file cannot call an unexported function directly.
func startOfDayForTest(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
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
	// Guards against a vacuous pass: if HandleStart silently sent nothing at
	// all for a dead nonce, every entry in answers would be "", the equality
	// check below would pass trivially, and the substring check after it
	// would also pass (an empty string contains neither "/sign-in/" nor
	// "/sign-up/"). A dead nonce answering nothing is itself a defect --
	// HandleStart's own doc comment requires an ordinary refusal to always
	// answer in the chat, since the poller drops the update the instant a
	// non-nil error comes back instead.
	if answers[0] == "" {
		t.Fatal("no message was sent for a dead nonce; the identical-answer checks below would pass vacuously")
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
	deadNonce := deadNonceAnswer(t)

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
	if got := doubles.sender.lastTo(600); got != deadNonce {
		t.Fatalf("rate-limited answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
	}
}

// Exactly telegramLinksPerHourLimit (3) redemptions within an hour must still
// succeed -- the limit is 3/hour, not 2/hour. Every other rate-limit test in
// this file starts a chat at 0 or 3 prior redemptions; without this one,
// changing `count > telegramLinksPerHourLimit` to
// `count >= telegramLinksPerHourLimit` -- the likeliest "make this consistent
// with auth.go's >= check" edit anyone will ever make to this file -- would
// silently drop the real limit to 2/hour while every other test in this file
// stayed green.
func TestHandleStartAllowsTheThirdRedemptionWithinAnHour(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(602, "user-3")
	doubles.links.recordRedemptions(602, 2, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 602, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.magicLinks.countFor("user-3") != 1 {
		t.Fatal("the third redemption within an hour was refused; the limit is 3 per hour, not 2")
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
	deadNonce := deadNonceAnswer(t)

	svc, doubles := newTelegramAuthService(t)
	cutoff := doubles.clock.Now().Add(-time.Hour)
	doubles.links.recordRedemptions(700, 3, cutoff)
	raw := doubles.links.mintLive(t, doubles.clock.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 700, raw); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(700); got != deadNonce {
		t.Fatalf("boundary-redemption answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
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
	deadNonce := deadNonceAnswer(t)

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
	if got != deadNonce {
		t.Fatalf("ceiling answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
	}

	// R5 requires a calendar-day cutoff (startOfDay(now)), not now itself and
	// not a rolling 24-hour window -- see signup.go's Request doc comment for
	// why. setGlobalCount's override answers CountSince before its since
	// argument is ever inspected, so the assertions above pass regardless of
	// what sendSignUp actually passed as the cutoff; this is the assertion
	// that catches a wrong cutoff. midnight is recomputed test-side because
	// startOfDay is unexported.
	midnight := startOfDayForTest(doubles.clock.Now())
	if got := doubles.signups.lastCountSinceArg(); !got.Equal(midnight) {
		t.Fatalf("CountSince since = %v, want midnight %v (startOfDay(now), not now or a rolling window)", got, midnight)
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
