package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
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
