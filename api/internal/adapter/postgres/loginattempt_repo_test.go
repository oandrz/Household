package postgres_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func createHouseholdForAttemptTest(t *testing.T, db *postgres.DB, name string) domain.Household {
	t.Helper()
	h, err := postgres.NewHouseholdRepo(db).Create(context.Background(), domain.Household{
		Timezone: "UTC",
		Name:     name, FamilyName: name,
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household %s: %v", name, err)
	}
	return h
}

// TestReserveFailureForAnAddressCountsOnlyThatAddress proves the promise
// sign-in leans on for an address that matches no user: the earlier
// failures are that address's alone, inside the window, and unsuccessful --
// so a stranger's countdown looks identical to a member's and one
// stranger's guesses never spend another's.
func TestReserveFailureForAnAddressCountsOnlyThatAddress(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)

	base := time.Now().UTC()

	// Two failures for the target address inside the window, one outside it.
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-40*time.Minute)); err != nil {
		t.Fatalf("Record (outside window): %v", err)
	}
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-5*time.Minute)); err != nil {
		t.Fatalf("Record (inside window): %v", err)
	}
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, base.Add(-1*time.Minute)); err != nil {
		t.Fatalf("Record (inside window): %v", err)
	}
	// A successful attempt for the same address, inside the window -- must
	// not be counted as a failure.
	if err := attempts.Record(ctx, nil, nil, "stranger@example.com", true, base.Add(-2*time.Minute)); err != nil {
		t.Fatalf("Record (succeeded): %v", err)
	}
	// Failures for a different address entirely, inside the window -- must
	// not leak into the target address's count.
	for _, offset := range []time.Duration{-3 * time.Minute, -2 * time.Minute} {
		if err := attempts.Record(ctx, nil, nil, "someone-else@example.com", false, base.Add(offset)); err != nil {
			t.Fatalf("Record (other address): %v", err)
		}
	}

	first, err := attempts.ReserveFailure(ctx, nil, nil, "stranger@example.com", base, base.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("ReserveFailure: %v", err)
	}
	if len(first.Earlier) != 2 {
		t.Fatalf("earlier failures = %d, want 2 — the 40-minute-old failure is outside the window, the "+
			"successful attempt must not count, the other address's failures must not leak in, and the "+
			"reservation must not count itself", len(first.Earlier))
	}

	second, err := attempts.ReserveFailure(ctx, nil, nil, "stranger@example.com", base, base.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("second ReserveFailure: %v", err)
	}
	if len(second.Earlier) != 3 {
		t.Fatalf("earlier failures for the second reservation = %d, want 3 — the first reservation is "+
			"a failure on record until its password check says otherwise", len(second.Earlier))
	}
}

// TestReserveFailureForAHouseholdCountsOnlyThatHousehold: with a household,
// the earlier failures are the household's, whichever member's address they
// were made against, and nobody else's.
func TestReserveFailureForAHouseholdCountsOnlyThatHousehold(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)
	ours := createHouseholdForAttemptTest(t, db, "Ours")
	theirs := createHouseholdForAttemptTest(t, db, "Theirs")

	base := time.Now().UTC()
	since := base.Add(-15 * time.Minute)

	// Two members of our household, one failure each.
	for _, email := range []string{"a@ours.test", "b@ours.test"} {
		if err := attempts.Record(ctx, &ours.ID, nil, email, false, base.Add(-time.Minute)); err != nil {
			t.Fatalf("Record (ours): %v", err)
		}
	}
	// Another household's failure, and a failure for one of our addresses
	// recorded with no household at all.
	if err := attempts.Record(ctx, &theirs.ID, nil, "c@theirs.test", false, base.Add(-time.Minute)); err != nil {
		t.Fatalf("Record (theirs): %v", err)
	}
	if err := attempts.Record(ctx, nil, nil, "a@ours.test", false, base.Add(-time.Minute)); err != nil {
		t.Fatalf("Record (no household): %v", err)
	}

	reserved, err := attempts.ReserveFailure(ctx, &ours.ID, nil, "a@ours.test", base, since)
	if err != nil {
		t.Fatalf("ReserveFailure: %v", err)
	}
	if len(reserved.Earlier) != 2 {
		t.Fatalf("earlier failures = %d, want 2 — both members' failures count, the other household's "+
			"and the household-less one do not", len(reserved.Earlier))
	}
}

// TestWithdrawRemovesTheReservedFailure: a withdrawn reservation is gone, as
// if the attempt had never been made, and withdrawing twice is harmless.
func TestWithdrawRemovesTheReservedFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)
	h := createHouseholdForAttemptTest(t, db, "H")

	base := time.Now().UTC()
	since := base.Add(-15 * time.Minute)

	reserved, err := attempts.ReserveFailure(ctx, &h.ID, nil, "a@b.c", base, since)
	if err != nil {
		t.Fatalf("ReserveFailure: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := attempts.Withdraw(ctx, reserved.ID); err != nil {
			t.Fatalf("Withdraw #%d: %v", i+1, err)
		}
	}

	failures, err := attempts.FailuresSince(ctx, h.ID, since)
	if err != nil {
		t.Fatalf("FailuresSince: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("failures on record after Withdraw = %d, want 0", len(failures))
	}
}

// TestReservationsMadeTogetherEachSeeTheOnesBeforeThem is the property the
// lockout depends on under load: reservations made at the same moment are
// put in one order, and each sees exactly the ones ahead of it. So forty
// simultaneous attempts see 0, 1, 2 ... earlier failures, one each, up to
// the cap on how many one list returns (usecase.MaxEarlierFailures). With
// a limit of three, that is what makes exactly the first three "the first
// three", however they arrive.
//
// Without the ledger lock, attempts that overlap either all see nothing
// (read before write) or count against each other in both directions
// (write, then read): some numbers repeat and others are missing.
func TestReservationsMadeTogetherEachSeeTheOnesBeforeThem(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)
	h := createHouseholdForAttemptTest(t, db, "H")

	const callers = 40
	base := time.Now().UTC()
	since := base.Add(-15 * time.Minute)

	cases := map[string]func(caller int) (usecase.FailureReservation, error){
		"for a household": func(int) (usecase.FailureReservation, error) {
			return attempts.ReserveFailure(ctx, &h.ID, nil, "a@b.c", base, since)
		},
		// login_attempts.email ignores case, so the order must too: two
		// spellings of one address are one line, not two.
		"for an address, however it is spelled": func(caller int) (usecase.FailureReservation, error) {
			email := "stranger@example.com"
			if caller%2 == 0 {
				email = "Stranger@Example.COM"
			}
			return attempts.ReserveFailure(ctx, nil, nil, email, base, since)
		},
	}
	for name, reserve := range cases {
		t.Run(name, func(t *testing.T) {
			seen := make([]int, callers)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < callers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					reserved, err := reserve(i)
					if err != nil {
						t.Errorf("ReserveFailure: %v", err)
						return
					}
					seen[i] = len(reserved.Earlier)
				}()
			}
			close(start)
			wg.Wait()
			if t.Failed() {
				return
			}

			// Each list is capped at the newest MaxEarlierFailures, so the
			// places in line read 0, 1 ... cap-1 and then cap for everyone
			// behind.
			sort.Ints(seen)
			for i, n := range seen {
				if n != min(i, usecase.MaxEarlierFailures) {
					t.Fatalf("simultaneous reservations did not each get their own place in line; "+
						"what each one saw, sorted: %v", seen)
				}
			}
		})
	}
}
