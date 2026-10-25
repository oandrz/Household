package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
)

// seedMember inserts a user and their membership in a household and returns
// the membership id, which is what Confirm records.
func seedMember(t *testing.T, ctx context.Context, db *postgres.DB, householdID, name, email string) string {
	t.Helper()
	var userID, membershipID string
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (email, display_name, avatar_initial) VALUES ($1, $2, $3) RETURNING id`,
		email, name, name[:1]).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO memberships (household_id, user_id, role, capabilities)
		 VALUES ($1, $2, 'owner', '{calendar,chores,money,marriage}') RETURNING id`,
		householdID, userID).Scan(&membershipID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return membershipID
}

func TestConfirmIsIdempotentAndKeepsTheFirstConfirmer(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewCashflowConfirmationRepo(db)
	h := insertTestHousehold(t, db)
	ann := seedMember(t, ctx, db, h, "Ann", "ann@example.test")
	ben := seedMember(t, ctx, db, h, "Ben", "ben@example.test")
	month := day("2026-03-01")
	firstAt := time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)

	first, err := repo.Confirm(ctx, h, month, ann, firstAt)
	if err != nil {
		t.Fatalf("first Confirm: %v", err)
	}
	second, err := repo.Confirm(ctx, h, month, ben, firstAt.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("second Confirm: %v", err)
	}

	if first.ConfirmedByName != "Ann" || second.ConfirmedByName != "Ann" {
		t.Fatalf("names = %q, %q, want Ann both times", first.ConfirmedByName, second.ConfirmedByName)
	}
	if !second.ConfirmedAt.Equal(firstAt) || !first.ConfirmedAt.Equal(firstAt) {
		t.Fatalf("confirmed_at = %v, %v, want %v both times", first.ConfirmedAt, second.ConfirmedAt, firstAt)
	}
	if !second.Month.Equal(month) {
		t.Fatalf("month = %v, want %v", second.Month, month)
	}
	list, err := repo.List(ctx, h)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v, want exactly one row", list, err)
	}
}

func TestUnconfirmOfAMissingMonthIsNotAnError(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewCashflowConfirmationRepo(db)
	h := insertTestHousehold(t, db)
	if err := repo.Unconfirm(ctx, h, day("2026-03-01")); err != nil {
		t.Fatalf("Unconfirm of a month never confirmed: %v", err)
	}
}

func TestUnconfirmRemovesTheMark(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewCashflowConfirmationRepo(db)
	h := insertTestHousehold(t, db)
	ann := seedMember(t, ctx, db, h, "Ann", "ann@example.test")
	if _, err := repo.Confirm(ctx, h, day("2026-03-01"), ann, time.Now()); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := repo.Unconfirm(ctx, h, day("2026-03-01")); err != nil {
		t.Fatalf("Unconfirm: %v", err)
	}
	if list, err := repo.List(ctx, h); err != nil || len(list) != 0 {
		t.Fatalf("List = %v, %v, want empty", list, err)
	}
}

func TestConfirmationsAreScopedToTheirHousehold(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewCashflowConfirmationRepo(db)
	x := insertTestHousehold(t, db)
	y := insertTestHousehold(t, db)
	ann := seedMember(t, ctx, db, x, "Ann", "ann@example.test")
	if _, err := repo.Confirm(ctx, x, day("2026-03-01"), ann, time.Now()); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	list, err := repo.List(ctx, y)
	if err != nil || len(list) != 0 {
		t.Fatalf("household Y List = %v, %v, want empty", list, err)
	}
	// Unconfirming from Y must not touch X's row either.
	if err := repo.Unconfirm(ctx, y, day("2026-03-01")); err != nil {
		t.Fatalf("Unconfirm: %v", err)
	}
	if list, _ := repo.List(ctx, x); len(list) != 1 {
		t.Fatalf("household X lost its confirmation: %v", list)
	}
}

func TestAConfirmationWhoseMemberLeftReadsAsAnEmptyName(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := postgres.NewCashflowConfirmationRepo(db)
	h := insertTestHousehold(t, db)
	ann := seedMember(t, ctx, db, h, "Ann", "ann@example.test")
	if _, err := repo.Confirm(ctx, h, day("2026-03-01"), ann, time.Now()); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM memberships WHERE id = $1`, ann); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	list, err := repo.List(ctx, h)
	if err != nil || len(list) != 1 || list[0].ConfirmedByName != "" {
		t.Fatalf("List = %+v, %v, want one row with an empty name", list, err)
	}
}

func TestTheDatabaseRefusesAMidMonthDate(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	h := insertTestHousehold(t, db)
	_, err := db.Pool().Exec(ctx,
		`INSERT INTO cashflow_month_confirmations (household_id, month) VALUES ($1, DATE '2026-03-14')`, h)
	if err == nil {
		t.Fatal("a mid-month date was accepted")
	}
}
