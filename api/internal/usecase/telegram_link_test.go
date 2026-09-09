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

func TestStartMintsALinkNonceCarryingTheMember(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)

	got, err := svc.Start(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}
	if !strings.HasPrefix(got.URL, "https://t.me/HearthBot?start=") {
		t.Fatalf("URL = %q, want a t.me deep link", got.URL)
	}
	raw := strings.TrimPrefix(got.URL, "https://t.me/HearthBot?start=")
	if doubles.links.hasRaw(raw) {
		t.Fatal("the raw nonce was stored; it must be stored hashed")
	}
	if n, _ := doubles.links.CountMintsSince(context.Background(), "user-1", doubles.clock.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("mints for user-1 = %d, want 1", n)
	}
}

func TestStatusIsPendingOnlyAfterAChatRedeemsIt(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")

	got, err := svc.Status(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Status() = %v, want nil", err)
	}
	if got.Status != "waiting" {
		t.Fatalf("Status = %q, want \"waiting\" before any chat arrives", got.Status)
	}

	doubles.links.redeem(start.ID, 701, "andreas")
	got, _ = svc.Status(context.Background(), "user-1", start.ID)
	if got.Status != "pending" || got.ChatUsername != "andreas" {
		t.Fatalf("Status = %+v, want pending naming andreas", got)
	}
}

func TestStatusHidesAnotherMembersLink(t *testing.T) {
	svc, _ := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")

	// 404, not 403: a row id must not be testable for existence.
	if _, err := svc.Status(context.Background(), "user-2", start.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Status for another member = %v, want domain.ErrNotFound", err)
	}
}

func TestConfirmWritesTheBinding(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 702, "andreas")

	got, err := svc.Confirm(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	if got.ChatID != 702 || got.ChatUsername != "andreas" {
		t.Fatalf("binding = %+v, want chat 702 (andreas)", got)
	}
	if id, _ := doubles.accounts.ByChatID(context.Background(), 702); id != "user-1" {
		t.Fatalf("ByChatID = %q, want user-1", id)
	}
}

func TestConfirmRefusesAnotherMembersLink(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 703, "thief")

	// The mutation target: delete the ownership check in Confirm and this is
	// the test that goes red.
	if _, err := svc.Confirm(context.Background(), "user-2", start.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Confirm by another member = %v, want domain.ErrNotFound", err)
	}
	if _, err := doubles.accounts.ByChatID(context.Background(), 703); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("a binding was written for a link the caller does not own")
	}
}

func TestConfirmRefusesAfterExpiry(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 704, "andreas")
	doubles.clock.Advance(11 * time.Minute)

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramLinkNotPending) {
		t.Fatalf("Confirm after expiry = %v, want domain.ErrTelegramLinkNotPending", err)
	}
}

func TestConfirmRefusesAChatSomeoneElseHasBound(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(705, "user-9")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 705, "andreas")

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramChatTaken) {
		t.Fatalf("Confirm = %v, want domain.ErrTelegramChatTaken", err)
	}
}

func TestConfirmRefusesWhenTheMemberAlreadyHasAChat(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(706, "user-1")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 707, "andreas")

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramAlreadyLinked) {
		t.Fatalf("Confirm = %v, want domain.ErrTelegramAlreadyLinked", err)
	}
}

// TestConfirmRacingItsOwnEarlierConfirmReturnsTheSameBinding covers the
// double-click / two-tabs-polling-one-link race: this call's own pre-checks
// see nothing bound, but by the time its own Create reaches the database,
// its own earlier request already committed the identical row. The loser
// must not be told the chat "belongs to another account" -- that would be
// false, since it is the same account.
func TestConfirmRacingItsOwnEarlierConfirmReturnsTheSameBinding(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 711, "andreas")

	doubles.accounts.failNextCreateWithConflict(usecase.TelegramBinding{
		UserID: "user-1", ChatID: 711, ChatUsername: "andreas",
	})

	got, err := svc.Confirm(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Confirm() = %v, want nil -- losing a race against its own earlier confirm is not an error", err)
	}
	if got.ChatID != 711 || got.ChatUsername != "andreas" {
		t.Fatalf("binding = %+v, want chat 711 (andreas)", got)
	}
}

// TestConfirmRacingASecondPendingLinkForTheSameUserReportsAlreadyLinked
// covers the other race: two pending links for the same user confirmed
// concurrently. By the time this Create reaches the database, the user is
// already bound to a *different* chat, so the chat side was never the
// problem -- the user-side UNIQUE is the one that fired.
func TestConfirmRacingASecondPendingLinkForTheSameUserReportsAlreadyLinked(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 802, "andreas")

	doubles.accounts.failNextCreateWithConflict(usecase.TelegramBinding{
		UserID: "user-1", ChatID: 801, ChatUsername: "other-chat",
	})

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); !errors.Is(err, domain.ErrTelegramAlreadyLinked) {
		t.Fatalf("Confirm() = %v, want domain.ErrTelegramAlreadyLinked", err)
	}
}

func TestStatusStaysConnectedAfterTheLinkExpires(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 708, "andreas")
	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	doubles.clock.Advance(11 * time.Minute)

	// Binding first, expiry last. A panel still polling when the nonce
	// expires must not be told its working connection expired.
	got, _ := svc.Status(context.Background(), "user-1", start.ID)
	if got.Status != "connected" {
		t.Fatalf("Status = %q, want \"connected\"", got.Status)
	}
}

// TestStatusRefusesWhenTheChatIsBoundToSomeoneElse is the first of Status's
// two "refused" branches: a chat redeemed this link, but that chat already
// belongs to a different Hearth account.
func TestStatusRefusesWhenTheChatIsBoundToSomeoneElse(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(821, "user-9")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 821, "andreas")

	got, err := svc.Status(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Status() = %v, want nil", err)
	}
	if got.Status != "refused" {
		t.Fatalf("Status = %q, want \"refused\"", got.Status)
	}
	if got.Reason != usecase.TelegramLinkReasonChatTaken {
		t.Fatalf("Reason = %q, want %q", got.Reason, usecase.TelegramLinkReasonChatTaken)
	}
}

// TestStatusRefusesWhenTheMemberAlreadyHasADifferentChat is Status's other
// "refused" branch: the chat redeemed this link cleanly, but this member
// already has a different chat connected.
func TestStatusRefusesWhenTheMemberAlreadyHasADifferentChat(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(822, "user-1")
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.links.redeem(start.ID, 823, "andreas")

	got, err := svc.Status(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Status() = %v, want nil", err)
	}
	if got.Status != "refused" {
		t.Fatalf("Status = %q, want \"refused\"", got.Status)
	}
	if got.Reason != usecase.TelegramLinkReasonAlreadyLinked {
		t.Fatalf("Reason = %q, want %q", got.Reason, usecase.TelegramLinkReasonAlreadyLinked)
	}
}

// TestStatusCarriesNoReasonForWaitingPendingOrConnected complements the two
// refused-branch tests above: Reason exists to give the panel a code for
// the one status that needs one, so this proves the other statuses don't
// carry a stale one forward.
func TestStatusCarriesNoReasonForWaitingPendingOrConnected(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")

	if got, err := svc.Status(context.Background(), "user-1", start.ID); err != nil || got.Status != "waiting" || got.Reason != "" {
		t.Fatalf("waiting: Status, err = %+v, %v, want waiting with no reason", got, err)
	}

	doubles.links.redeem(start.ID, 831, "andreas")
	if got, err := svc.Status(context.Background(), "user-1", start.ID); err != nil || got.Status != "pending" || got.Reason != "" {
		t.Fatalf("pending: Status, err = %+v, %v, want pending with no reason", got, err)
	}

	if _, err := svc.Confirm(context.Background(), "user-1", start.ID); err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	if got, err := svc.Status(context.Background(), "user-1", start.ID); err != nil || got.Status != "connected" || got.Reason != "" {
		t.Fatalf("connected: Status, err = %+v, %v, want connected with no reason", got, err)
	}
}

// TestStatusCarriesNoReasonWhenExpired is the fourth status
// TestStatusCarriesNoReasonForWaitingPendingOrConnected does not reach: an
// unconsumed row past its expiry needs no explanation, just "start again".
func TestStatusCarriesNoReasonWhenExpired(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	start, _ := svc.Start(context.Background(), "user-1")
	doubles.clock.Advance(11 * time.Minute)

	got, err := svc.Status(context.Background(), "user-1", start.ID)
	if err != nil {
		t.Fatalf("Status() = %v, want nil", err)
	}
	if got.Status != "expired" || got.Reason != "" {
		t.Fatalf("Status = %+v, want expired with no reason", got)
	}
}

func TestStartRefusesTheFourthMintInAnHour(t *testing.T) {
	svc, _ := newTelegramLinkService(t)
	for i := 0; i < 3; i++ {
		if _, err := svc.Start(context.Background(), "user-1"); err != nil {
			t.Fatalf("Start() #%d = %v, want nil", i+1, err)
		}
	}
	if _, err := svc.Start(context.Background(), "user-1"); !errors.Is(err, domain.ErrTelegramMintsRateLimited) {
		t.Fatalf("fourth Start() = %v, want domain.ErrTelegramMintsRateLimited", err)
	}
}

func TestUnlinkRefusesAnAccountWithNoEmail(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.users.addTelegramOnly("user-3") // email ""
	doubles.accounts.bind(709, "user-3")

	if err := svc.Unlink(context.Background(), "user-3"); !errors.Is(err, domain.ErrTelegramUnlinkWouldLockOut) {
		t.Fatalf("Unlink() = %v, want domain.ErrTelegramUnlinkWouldLockOut", err)
	}
	if id, _ := doubles.accounts.ByChatID(context.Background(), 709); id != "user-3" {
		t.Fatal("the binding was removed; that account would have no way back in")
	}
}

func TestUnlinkRemovesTheBinding(t *testing.T) {
	svc, doubles := newTelegramLinkService(t)
	doubles.accounts.bind(710, "user-1") // user-1 has an email in the fixture

	if err := svc.Unlink(context.Background(), "user-1"); err != nil {
		t.Fatalf("Unlink() = %v, want nil", err)
	}
	if _, err := doubles.accounts.ByChatID(context.Background(), 710); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("binding after Unlink = %v, want gone", err)
	}
}
