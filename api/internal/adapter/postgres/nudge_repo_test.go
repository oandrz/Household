package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// bindChat creates a user, a membership with the given role and capabilities
// in householdID, and binds chatID to the user.
func bindChat(t *testing.T, db *postgres.DB, householdID string, chatID int64, role string, caps []string) {
	t.Helper()
	ctx := context.Background()
	user, err := postgres.NewUserRepo(db).Create(ctx, "", "", "Someone")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities) VALUES ($1, $2, $3, $4)`,
		householdID, user.ID, role, caps); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2)`, user.ID, chatID); err != nil {
		t.Fatalf("insert telegram account: %v", err)
	}
}

// The recipients query is the authorisation for the outbound direction: a
// digest carries money, so only an owner with Money, in a chat that has not
// opted out, may receive one. Each excluded shape here is one predicate.
func TestNudgeRecipientsAreOwnersWithMoneyWhoHaveNotOptedOut(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewNudgeRepo(db)
	household := insertTestHousehold(t, db)

	all := []string{"calendar", "chores", "money", "marriage"}
	bindChat(t, db, household, 9001, "owner", all)                 // included
	bindChat(t, db, household, 9002, "limited", []string{"money"}) // limited: excluded
	bindChat(t, db, household, 9004, "owner", all)                 // opted out below
	// "Owner without Money" cannot be inserted: owners_hold_all_capabilities
	// forbids it at the schema, so the capability predicate in the query is
	// belt-and-braces against a future relaxation, mirrored on the routes.
	if err := repo.SetEnabled(ctx, 9004, false); err != nil {
		t.Fatalf("SetEnabled(false) = %v", err)
	}
	// An owner with no telegram row is not a recipient because there is no
	// chat to send to; the join expresses that without a predicate.

	got, err := repo.Recipients(ctx)
	if err != nil {
		t.Fatalf("Recipients() = %v", err)
	}
	var chats []int64
	for _, r := range got {
		if r.HouseholdID == household {
			chats = append(chats, r.ChatID)
			if r.Currency != "SGD" || r.MembershipID == "" {
				t.Errorf("recipient %+v lacks currency or membership", r)
			}
		}
	}
	if len(chats) != 1 || chats[0] != 9001 {
		t.Fatalf("recipients for the household = %v, want [9001]", chats)
	}

	// /nudges on brings the chat back; an unbound chat is ErrNotFound.
	if err := repo.SetEnabled(ctx, 9004, true); err != nil {
		t.Fatalf("SetEnabled(true) = %v", err)
	}
	if err := repo.SetEnabled(ctx, 424242, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetEnabled on an unbound chat = %v, want ErrNotFound", err)
	}
}

func TestNudgeClaimIsAtMostOncePerChatHouseholdAndDay(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewNudgeRepo(db)
	household := insertTestHousehold(t, db)
	day := time.Date(2026, 9, 8, 9, 0, 0, 0, time.FixedZone("SGT", 8*3600))

	first, err := repo.Claim(ctx, 777, household, day)
	if err != nil || !first {
		t.Fatalf("first Claim() = %v, %v; want true", first, err)
	}
	second, err := repo.Claim(ctx, 777, household, day.Add(3*time.Hour))
	if err != nil || second {
		t.Fatalf("second Claim() the same day = %v, %v; want false", second, err)
	}
	next, err := repo.Claim(ctx, 777, household, day.AddDate(0, 0, 1))
	if err != nil || !next {
		t.Fatalf("Claim() the next day = %v, %v; want true", next, err)
	}

	// A failed send releases the claim so the next tick can try again.
	if err := repo.Release(ctx, 777, household, day); err != nil {
		t.Fatalf("Release() = %v", err)
	}
	again, err := repo.Claim(ctx, 777, household, day)
	if err != nil || !again {
		t.Fatalf("Claim() after Release() = %v, %v; want true", again, err)
	}

	pruned, err := repo.Prune(ctx, day.AddDate(0, 0, 1))
	if err != nil || pruned < 1 {
		t.Fatalf("Prune() = %d, %v; want at least the first day's row gone", pruned, err)
	}
}
