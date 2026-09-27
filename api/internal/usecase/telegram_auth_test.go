package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
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
	// hardcoded here since this file cannot see it. doubles.clock never
	// advances on its own, so this is exact equality, not a tolerance check --
	// a service shipping a wrong ExpiresAt while still passing the right
	// value to Links.Create would pass every other assertion here.
	if want := doubles.clock.Now().Add(10 * time.Minute); !link.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", link.ExpiresAt, want)
	}
}

func TestHandleStartSendsASignInLinkToAKnownChat(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(501, "user-1")
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 501, raw, ""); err != nil {
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

	if err := svc.HandleStart(context.Background(), 777, raw, ""); err != nil {
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

// HandleStart's username parameter must actually reach Links.Consume, not
// just sit in a log line -- the redeemed row is what the confirm screen
// reads back to name the chat someone is about to approve.
func TestHandleStartForwardsTheSenderNameToConsume(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 501, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	row, err := doubles.links.ByID(context.Background(), string(doubles.tokens.HashToken(raw)))
	if err != nil {
		t.Fatalf("ByID() = %v, want nil", err)
	}
	if row.ChatUsername != "andreas" {
		t.Fatalf("ChatUsername = %q, want %q", row.ChatUsername, "andreas")
	}
}

// deadNonceAnswer computes the dead-link baseline via a fresh fixture and an
// unknown nonce, so every test needing "byte-identical to an ordinary dead
// nonce" uses the same baseline rather than substring-matching text this
// package cannot see (telegramDeadLinkMessage is unexported). A substring
// check would still pass if extra text leaked alongside it -- an enumeration
// oracle -- so only byte equality against this baseline actually pins it.
func deadNonceAnswer(t *testing.T) string {
	t.Helper()
	svc, doubles := newTelegramAuthService(t)
	const probeChatID = int64(1)
	if err := svc.HandleStart(context.Background(), probeChatID, "never-minted", ""); err != nil {
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
		if err := svc.HandleStart(context.Background(), 900, raw, ""); err != nil {
			t.Fatalf("HandleStart() = %v, want nil", err)
		}
		answers = append(answers, doubles.sender.lastTo(900))
	}
	// Guards against a vacuous pass: if HandleStart sent nothing at all for a
	// dead nonce, every entry would be "", and both the equality and
	// substring checks below would pass trivially. Answering nothing is
	// itself a defect -- HandleStart's doc comment requires an answer in the
	// chat, since the poller drops the update on any non-nil error.
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

	if err := svc.HandleStart(context.Background(), 600, raw, ""); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.magicLinks.countFor("user-2") != 0 {
		t.Fatal("a magic link was minted for a rate-limited chat")
	}
	if got := doubles.sender.lastTo(600); got != deadNonce {
		t.Fatalf("rate-limited answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
	}
}

// Exactly telegramLinksPerHourLimit (3) redemptions within an hour must
// still succeed -- the limit is 3/hour, not 2/hour. Every other rate-limit
// test here starts a chat at 0 or 3 prior redemptions, so without this one,
// changing `count > telegramLinksPerHourLimit` to `>=` -- the likeliest
// "make this consistent with auth.go" edit -- would silently drop the limit
// to 2/hour while every other test stayed green.
func TestHandleStartAllowsTheThirdRedemptionWithinAnHour(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(602, "user-3")
	doubles.links.recordRedemptions(602, 2, time.Now().Add(-time.Minute))
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 602, raw, ""); err != nil {
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

	_ = svc.HandleStart(context.Background(), 601, raw, "")
	if !doubles.links.isConsumed(raw) {
		t.Fatal("a rate-limited attempt left its nonce unspent")
	}
}

// CountLinksSince's real SQL boundary is `consumed_at >= since` (inclusive --
// see queries/telegram.sql). A redemption landing exactly on the hour-old
// cutoff must still count toward the limit; an exclusive boundary would let
// a chat squeeze out one extra redemption at the edge of the window every
// hour.
func TestHandleStartRateLimitCountsARedemptionExactlyOnTheSinceBoundary(t *testing.T) {
	deadNonce := deadNonceAnswer(t)

	svc, doubles := newTelegramAuthService(t)
	cutoff := doubles.clock.Now().Add(-time.Hour)
	doubles.links.recordRedemptions(700, 3, cutoff)
	raw := doubles.links.mintLive(t, doubles.clock.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 700, raw, ""); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(700); got != deadNonce {
		t.Fatalf("boundary-redemption answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
	}
}

// SignupService.Request's global daily ceiling counts rows in the signups
// table with no channel filter, and CreateForTelegram writes into that same
// table -- without a check here, a flood of Telegram sign-ups could exhaust
// the ceiling meant for email while staying unbounded itself. The refusal is
// pinned against a second fixture's unknown-nonce answer, not a substring
// match, so this cannot pass against an implementation that merely refuses
// with *some* message.
func TestHandleStartRefusesSignUpAtTheGlobalDailyCeilingWithTheSameAnswerAsADeadNonce(t *testing.T) {
	deadNonce := deadNonceAnswer(t)

	svc, doubles := newTelegramAuthService(t)
	// Set to exactly the limit, not one above it, to pin that the check is
	// >= (matching Request's own check) -- a > check would let one sign-up
	// through right at the ceiling every day.
	doubles.signups.setGlobalCount(usecase.SignupGlobalDailyLimit)
	raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 888, raw, ""); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.signups.telegramCount(888) != 0 {
		t.Fatal("a signup row was created over the global daily ceiling")
	}
	got := doubles.sender.lastTo(888)
	if got != deadNonce {
		t.Fatalf("ceiling answer = %q, want the identical dead-nonce answer %q", got, deadNonce)
	}

	// The cutoff must be startOfDay(now), not now itself and not a rolling
	// 24-hour window (see startOfDay's doc comment). setGlobalCount's
	// override answers CountSince before its since argument is inspected, so
	// the assertions above pass regardless of the real cutoff -- this is the
	// one that catches a wrong cutoff. midnight is recomputed test-side since
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

	if err := svc.HandleStart(context.Background(), 889, raw, ""); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if doubles.signups.telegramCount(889) != 1 {
		t.Fatalf("telegram signups created = %d, want 1", doubles.signups.telegramCount(889))
	}
	// telegramSenderDouble.lastTo returns "" for "nothing was sent", so a
	// mutation that stops sendSignUp calling say() on success -- provisioning
	// the row but telling nobody -- would leave the createCount assertion
	// above green without this check.
	if sent := doubles.sender.lastTo(889); !strings.Contains(sent, "/sign-up/") {
		t.Fatalf("message = %q, want the sign-up URL", sent)
	}
}

func TestHandleStartWithALinkNonceLeavesTheBindingUnwritten(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 601, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	// The whole design in one assertion: the chat is recorded, and the
	// binding is not written until the browser that minted the nonce says so.
	// A one-phase bind would make a leaked deep link an account takeover.
	if _, err := doubles.accounts.ByChatID(context.Background(), 601); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("binding after /start = %v, want it still unwritten", err)
	}
	if got := doubles.sender.lastTo(601); !strings.Contains(got, "confirm") {
		t.Fatalf("message = %q, want it to send the person back to Hearth to confirm", got)
	}
	if doubles.magicLinks.countFor("user-7") != 0 {
		t.Fatal("a magic link was minted; a link nonce must mint no token at all")
	}
}

// A chat that already belongs to someone else's account gets the same
// bland refusal as every other case a stolen nonce could probe with. It
// must not learn the target account exists, or that a different chat
// already holds it, and the existing binding must not move.
func TestHandleStartWithALinkNonceForAChatSomeoneElseOwnsSaysNothingUseful(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(602, "someone-else")
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 602, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(602); !strings.Contains(got, "Open Hearth") {
		t.Fatalf("message = %q, want the bland refusal", got)
	}
	if bound, err := doubles.accounts.ByChatID(context.Background(), 602); err != nil || bound != "someone-else" {
		t.Fatalf("binding for chat 602 = (%q, %v), want it untouched at %q", bound, err, "someone-else")
	}
	if doubles.magicLinks.countFor("user-7") != 0 {
		t.Fatal("a magic link was minted for a link nonce that was refused")
	}
}

// A chat already connected to the *same* user the nonce names is told so
// plainly, not with the bland refusal -- there is nothing to protect by
// hiding this from a chat that is already the account's own.
func TestHandleStartWithALinkNonceForAChatAlreadyConnectedToTheSameUserSaysSo(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(606, "user-7")
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 606, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(606); !strings.Contains(got, "already connected") {
		t.Fatalf("message = %q, want the already-connected message", got)
	}
	if bound, err := doubles.accounts.ByChatID(context.Background(), 606); err != nil || bound != "user-7" {
		t.Fatalf("binding for chat 606 = (%q, %v), want it untouched at %q", bound, err, "user-7")
	}
	if doubles.magicLinks.countFor("user-7") != 0 {
		t.Fatal("a magic link was minted for a link nonce that was refused")
	}
}

// A link nonce redeemed from a chat with no binding of its own is still
// refused with the bland line, not told to go confirm, when the nonce's
// user already has a *different* chat bound. Otherwise the person would be
// sent back to Hearth believing the link worked, only for Confirm to refuse
// them there -- exactly the round trip this row exists to save.
func TestHandleStartWithALinkNonceForAUserAlreadyBoundToADifferentChatSaysNothingUseful(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	doubles.accounts.bind(604, "user-7")
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 605, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	if got := doubles.sender.lastTo(605); !strings.Contains(got, "Open Hearth") {
		t.Fatalf("message = %q, want the bland refusal", got)
	}
	if _, err := doubles.accounts.ByChatID(context.Background(), 605); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("binding for chat 605 = %v, want it still unwritten", err)
	}
	if bound, err := doubles.accounts.ByChatID(context.Background(), 604); err != nil || bound != "user-7" {
		t.Fatalf("binding for chat 604 = (%q, %v), want it untouched at %q", bound, err, "user-7")
	}
	if doubles.magicLinks.countFor("user-7") != 0 {
		t.Fatal("a magic link was minted for a link nonce that was refused")
	}
}

func TestHandleStartAnswersALinkNonceBeforeTheRateLimitBites(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	// Four redemptions from one chat: the fourth is over the 3/hour limit.
	for i := 0; i < 3; i++ {
		raw := doubles.links.mintLive(t, time.Now().Add(10*time.Minute))
		_ = svc.HandleStart(context.Background(), 603, raw, "")
	}
	raw := doubles.links.mintLiveFor(t, "user-7", time.Now().Add(10*time.Minute))

	if err := svc.HandleStart(context.Background(), 603, raw, "andreas"); err != nil {
		t.Fatalf("HandleStart() = %v, want nil", err)
	}
	got := doubles.sender.lastTo(603)
	// The two ends of the flow must agree. If the limit ran first, the row
	// would be consumed and carrying a user id -- which the browser derives
	// as pending -- while the chat had been told the link was dead.
	if strings.Contains(got, "expired") {
		t.Fatalf("message = %q, want the confirm instruction, not the dead-link line", got)
	}
	// A positive assertion, not just the negative one above: an empty
	// message -- nothing sent at all -- would also fail to contain
	// "expired" and pass the check above vacuously.
	if !strings.Contains(got, "confirm") {
		t.Fatalf("message = %q, want the confirm instruction to actually have been sent", got)
	}
}

// The bot's inv_ branch: a first tap records a knock and is answered with
// the code, and every other outcome gets the one bland dead-link reply
// (telegramDeadLinkMessage), so a chat holding a link it may have stolen
// learns nothing by probing.
func TestStartWithAnInviteTokenRecordsOneKnock(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	ctx := context.Background()
	rawToken := doubles.seedTelegramInvite(t, defaultTestHouseholdID, "Christine")

	if err := svc.HandleStart(ctx, 4242, "inv_"+rawToken, "jane_t"); err != nil {
		t.Fatalf("HandleStart: %v", err)
	}
	said := doubles.sender.lastTo(4242)
	code := doubles.invites.knockCode(rawToken)
	if !strings.Contains(said, code) {
		t.Fatalf("the chat was told %q, which does not contain its code %q", said, code)
	}

	// One knock per link: a second tap -- by the same chat or another --
	// gets the dead-link reply and changes nothing.
	if err := svc.HandleStart(ctx, 9999, "inv_"+rawToken, "someone_else"); err != nil {
		t.Fatalf("second HandleStart: %v", err)
	}
	if got := doubles.sender.lastTo(9999); got != "That sign-in link has expired. Start again from the app." {
		t.Fatalf("second tap was told %q, want the bland dead-link reply", got)
	}
	if doubles.invites.knockChatID(rawToken) != 4242 {
		t.Fatal("the second tap overwrote the first knock")
	}
}

// Every refusal is the same sentence. Listed together because the point is
// that they are indistinguishable, which a test per case would not show.
func TestEveryRefusedInviteStartGetsTheSameReply(t *testing.T) {
	const dead = "That sign-in link has expired. Start again from the app."
	for name, setup := range map[string]func(t *testing.T, d *telegramDoubles) (payload string, chatID int64){
		"unknown token": func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_nosuchtoken", 4242 },
		"expired invite": func(t *testing.T, d *telegramDoubles) (string, int64) {
			return "inv_" + d.seedExpiredTelegramInvite(t), 4242
		},
		"email invite": func(t *testing.T, d *telegramDoubles) (string, int64) { return "inv_" + d.seedEmailInvite(t), 4242 },
		"already accepted": func(t *testing.T, d *telegramDoubles) (string, int64) {
			return "inv_" + d.seedAcceptedTelegramInvite(t), 4242
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, doubles := newTelegramAuthService(t)
			payload, chatID := setup(t, doubles)
			if err := svc.HandleStart(context.Background(), chatID, payload, "jane_t"); err != nil {
				t.Fatalf("HandleStart: %v", err)
			}
			if got := doubles.sender.lastTo(chatID); got != dead {
				t.Fatalf("got %q, want the one bland reply %q", got, dead)
			}
		})
	}
}

// A chat that already belongs to a Hearth account is the one refusal that
// is NOT bland -- it is safe because it is not about the link. It tells the
// tapper only about their own chat, which they could learn by sending
// /start with no payload anyway, and saves them tapping a link that will
// never work for them.
func TestAChatThatAlreadyBelongsToAnAccountIsToldSo(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	boundChat := doubles.seedBoundChat(t)
	rawToken := doubles.seedTelegramInvite(t, defaultTestHouseholdID, "Christine")

	if err := svc.HandleStart(context.Background(), boundChat, "inv_"+rawToken, "jane_t"); err != nil {
		t.Fatalf("HandleStart: %v", err)
	}
	if got := doubles.sender.lastTo(boundChat); got != "This Telegram account already belongs to a Hearth household." {
		t.Fatalf("got %q, want the already-belongs-to-a-household sentence", got)
	}
	// And it did not spend the link on its way to being refused: the check
	// runs before the guarded UPDATE, so somebody else can still knock.
	if doubles.invites.knockChatID(rawToken) != 0 {
		t.Fatal("a refused chat consumed the invite link")
	}
}

// Everything that is not an invite payload behaves exactly as it did
// before: the sign-in nonce, the sign-up path and the chat-link path are
// untouched.
func TestStartWithoutTheInvitePrefixIsUnchanged(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)
	nonce := doubles.seedSignInNonce(t)
	if err := svc.HandleStart(context.Background(), 4242, nonce, "jane_t"); err != nil {
		t.Fatalf("HandleStart: %v", err)
	}
	if !strings.Contains(doubles.sender.lastTo(4242), "/sign-up/") &&
		!strings.Contains(doubles.sender.lastTo(4242), "/sign-in/magic?token=") {
		t.Fatalf("an ordinary nonce no longer produces a link: %q", doubles.sender.lastTo(4242))
	}
}

// --- TelegramAuthService as InviteService's InviteChats -----------------

// SendSignIn is InviteChats' half of admitting a knocked chat: it mints
// through the same sendSignIn HandleStart uses, so there aren't two expiry
// rules and two rate limits drifting apart.
func TestSendSignInReusesTheOrdinaryMagicLinkPath(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)

	if err := svc.SendSignIn(context.Background(), 4242, "user-1"); err != nil {
		t.Fatalf("SendSignIn: %v", err)
	}
	sent := doubles.sender.lastTo(4242)
	if !strings.Contains(sent, "/sign-in/magic?token=") {
		t.Fatalf("message = %q, want it to carry a magic-link URL", sent)
	}
	if doubles.magicLinks.countFor("user-1") != 1 {
		t.Fatalf("magic links minted = %d, want 1", doubles.magicLinks.countFor("user-1"))
	}
}

// SendLinkCancelled is InviteChats' other half: the courtesy told to a
// knocked chat when the owner replaces the link.
func TestSendLinkCancelledTellsTheChatItsLinkIsDead(t *testing.T) {
	svc, doubles := newTelegramAuthService(t)

	if err := svc.SendLinkCancelled(context.Background(), 4242); err != nil {
		t.Fatalf("SendLinkCancelled: %v", err)
	}
	if got := doubles.sender.lastTo(4242); got != "That link is no longer valid. Ask whoever invited you for a new one." {
		t.Fatalf("got %q", got)
	}
}
