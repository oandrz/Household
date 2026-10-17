package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// Every counted kind answers, and answers zero for an empty household --
// the control that each query actually runs.
func TestEveryCountedKindHasAQuery(t *testing.T) {
	db := openTestDB(t)
	usage := postgres.NewHouseholdUsageRepo(db)
	household := insertTestHousehold(t, db)
	for _, k := range domain.CountedLimitKinds {
		n, err := usage.Count(context.Background(), household, k)
		if err != nil {
			t.Errorf("%s: %v", k, err)
		}
		if n != 0 {
			t.Errorf("%s: %d in an empty household, want 0", k, n)
		}
	}
}

// An unknown kind is an error, never a zero that would let the write through.
func TestAnUnknownKindIsAnErrorNotZero(t *testing.T) {
	db := openTestDB(t)
	usage := postgres.NewHouseholdUsageRepo(db)
	_, err := usage.Count(context.Background(), insertTestHousehold(t, db), domain.LimitKind("made_up"))
	if err == nil {
		t.Fatal("Count(made_up) succeeded, want an error")
	}
	_, err = usage.Count(context.Background(), insertTestHousehold(t, db), domain.LimitTransactionsInMonth)
	if err == nil {
		t.Fatal("Count(transactions_in_month) succeeded; that kind is per month, want an error")
	}
}

// Archived accounts count: they cannot be deleted, so archive-and-recreate
// must not get round the cap. Another household's rows never count.
func TestAccountsCountArchivedRowsAndOnlyThisHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	usage := postgres.NewHouseholdUsageRepo(db)
	household, _ := seedHouseholdAndAccount(t, ctx, db)
	seedHouseholdAndAccount(t, ctx, db) // another household, whose rows must not count
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO accounts (household_id, nickname, type, opening_balance_minor, opening_balance_currency,
		                       opening_balance_as_of, archived_at)
		 VALUES ($1, 'Old', 'cash', 0, 'SGD', DATE '2026-07-01', now())`, household); err != nil {
		t.Fatalf("insert archived account: %v", err)
	}
	n, err := usage.Count(ctx, household, domain.LimitAccounts)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("accounts = %d, want 2 (one live, one archived; the other household's not counted)", n)
	}
}

func TestTransactionsInMonthCountsOnlyThatMonth(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	usage := postgres.NewHouseholdUsageRepo(db)
	household, account := seedHouseholdAndAccount(t, ctx, db)
	for _, day := range []string{"2026-09-30", "2026-10-01", "2026-10-31", "2026-11-01"} {
		if _, err := db.Pool().Exec(ctx,
			`INSERT INTO transactions (household_id, kind, occurred_on, description, from_account_id,
			                           amount_minor, amount_currency)
			 VALUES ($1, 'expense', $2, 'x', $3, 100, 'SGD')`, household, day, account); err != nil {
			t.Fatalf("insert %s: %v", day, err)
		}
	}
	n, err := usage.TransactionsInMonth(ctx, household, time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("October = %d, want 2 (the 1st and the 31st)", n)
	}
	total, err := usage.Count(ctx, household, domain.LimitTransactions)
	if err != nil || total != 4 {
		t.Fatalf("total = %d, %v; want 4", total, err)
	}
}

// Pending invites exclude accepted and expired ones; active tokens exclude
// revoked and expired ones.
func TestPendingInvitesAndActiveTokensExcludeSpentRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	usage := postgres.NewHouseholdUsageRepo(db)
	household := insertTestHousehold(t, db)
	u, err := postgres.NewUserRepo(db).Create(ctx, "a@example.com", "", "A")
	if err != nil {
		t.Fatal(err)
	}
	user := u.ID
	for i, state := range []string{"pending", "accepted", "expired"} {
		accepted, expires := "NULL", "now() + interval '1 day'"
		switch state {
		case "accepted":
			accepted = "now()"
		case "expired":
			expires = "now() - interval '1 day'"
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO invites (household_id, email, name, role, token_hash, expires_at, accepted_at, invited_by)
			VALUES ($1, $2, 'X', 'limited', $3, `+expires+`, `+accepted+`, $4)`,
			household, state+"@example.com", []byte{byte(i)}, user); err != nil {
			t.Fatalf("insert %s invite: %v", state, err)
		}
	}
	for i, state := range []string{"live", "revoked", "expired"} {
		revoked, expires := "NULL", "now() + interval '1 day'"
		switch state {
		case "revoked":
			revoked = "now()"
		case "expired":
			expires = "now() - interval '1 day'"
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO api_tokens (user_id, household_id, name, token_hash, prefix, expires_at, revoked_at)
			VALUES ($1, $2, 'T', $3, 'p', `+expires+`, `+revoked+`)`,
			user, household, []byte{byte(10 + i)}); err != nil {
			t.Fatalf("insert %s token: %v", state, err)
		}
	}
	if n, err := usage.Count(ctx, household, domain.LimitPendingInvites); err != nil || n != 1 {
		t.Errorf("pending invites = %d, %v; want 1", n, err)
	}
	if n, err := usage.Count(ctx, household, domain.LimitActiveAPITokens); err != nil || n != 1 {
		t.Errorf("active tokens = %d, %v; want 1", n, err)
	}
}

// The database CHECKs count characters exactly as domain.CheckTextLength
// does: 200 emoji fit a description, 201 do not.
func TestTheDescriptionCheckCountsCharacters(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	household, account := seedHouseholdAndAccount(t, ctx, db)
	insert := func(desc string) error {
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO transactions (household_id, kind, occurred_on, description, from_account_id, amount_minor, amount_currency)
			 VALUES ($1, 'expense', DATE '2026-10-01', $2, $3, 100, 'SGD')`, household, desc, account)
		return err
	}
	if err := insert(strings.Repeat("😀", 200)); err != nil {
		t.Fatalf("200 emoji refused: %v", err)
	}
	if err := insert(strings.Repeat("😀", 201)); err == nil {
		t.Fatal("201 emoji accepted, want the CHECK to refuse it")
	}
}

// Every capped text column has a character-length CHECK at the domain's
// limit, so a write path that forgets the domain rule is still refused.
func TestEveryCappedTextColumnHasACharacterLengthCheck(t *testing.T) {
	db := openTestDB(t)
	cases := []struct {
		constraint string
		max        int
	}{
		{"households_name_length", domain.MaxNameLen},
		{"households_family_name_length", domain.MaxNameLen},
		{"users_display_name_length", domain.MaxNameLen},
		{"invites_name_length", domain.MaxNameLen},
		{"spaces_name_length", domain.MaxNameLen},
		{"accounts_nickname_length", domain.MaxNameLen},
		{"categories_name_length", domain.MaxNameLen},
		{"goals_name_length", domain.MaxNameLen},
		{"bills_name_length", domain.MaxNameLen},
		{"holdings_name_length", domain.MaxNameLen},
		{"holdings_unit_length", domain.MaxHoldingUnitLen},
		{"transactions_description_length", domain.MaxTransactionDescriptionLen},
		{"goal_contributions_note_length", domain.MaxNoteLen},
		{"holding_events_note_length", domain.MaxNoteLen},
		{"holding_valuations_note_length", domain.MaxNoteLen},
		{"holding_income_note_length", domain.MaxNoteLen},
		{"retro_actions_body_length", domain.MaxNoteLen},
	}
	for _, c := range cases {
		var def string
		err := db.Pool().QueryRow(context.Background(),
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, c.constraint).Scan(&def)
		if err != nil {
			t.Errorf("%s: %v", c.constraint, err)
			continue
		}
		if !strings.Contains(def, "char_length") || !strings.Contains(def, fmt.Sprintf("<= %d", c.max)) {
			t.Errorf("%s = %q, want char_length(...) <= %d", c.constraint, def, c.max)
		}
	}
}
