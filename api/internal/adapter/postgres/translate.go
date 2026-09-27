package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint
// violation (23505). See
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const pgUniqueViolation = "23505"

// uniqueConstraintErrors maps a unique constraint's NAME to the domain
// sentinel its violation means. translate matches by name, not just
// SQLSTATE 23505, so an unrelated 23505 cannot masquerade as one of these
// collisions -- an unlisted name falls through to ErrAlreadyExists. A new
// named collision is one entry here, not a new case in translate. Most
// names are Postgres's default, "<table>_<columns>_key"; the two indexes
// were named in their migrations.
var uniqueConstraintErrors = map[string]error{
	// categories' UNIQUE (household_id, name), 00005_transactions.sql.
	// CategoryRepository wants a sentinel specific to this constraint, not
	// the generic ErrAlreadyExists: Create and Rename both hit it on a name
	// collision, archived rows included, since archived_at is not part of
	// the key.
	"categories_household_id_name_key": domain.ErrCategoryNameTaken,

	// goals' UNIQUE (household_id, name), 00007_goals.sql. GoalRepository's
	// Create and Update both hit this on a name collision, archived rows
	// included -- the same archived-still-occupies-its-key rule categories
	// follow.
	"goals_household_id_name_key": domain.ErrGoalNameTaken,

	// goal_contributions' partial unique index on (household_id,
	// source_budget_month) WHERE source = 'budget_rollover', 00007_goals.sql.
	// A concurrent pair reaching the INSERT must not surface as a raw 23505
	// (BudgetRepo.RollOverToGoal's own doc). StampBudgetRollover's conditional
	// UPDATE normally catches this first; this index is the backstop that
	// makes a future path that skips that UPDATE fail safely, not silently.
	"goal_contributions_one_rollover_per_month": domain.ErrRolloverAlreadyDone,

	// bills' UNIQUE (household_id, name), 00008_bills.sql. BillRepository's
	// Create and Update doc comments: a name collision, archived rows
	// included, is ErrBillNameTaken -- the rule categories and goals follow.
	"bills_household_id_name_key": domain.ErrBillNameTaken,

	// The partial unique index from 00015_transaction_idempotency_key.sql.
	// TransactionRepository.Create's contract turns a hit into
	// ErrIdempotencyKeyInUse so the service can decide between a replay and
	// a 409.
	"transactions_household_idempotency_key": domain.ErrIdempotencyKeyInUse,

	// agreement_sections' UNIQUE (household_id, name), 00015_agreements.sql.
	// AgreementRepository.CreateSection's contract: the unique index decides
	// the collision, never a pre-read, so the screen can say "you already
	// have a section called that". Sections are never deleted, so a name is
	// never freed once taken.
	"agreement_sections_household_id_name_key": domain.ErrAgreementSectionNameTaken,

	// holdings' UNIQUE (account_id, name), 00019_holdings.sql. Scoped to the
	// ACCOUNT, not the household: the same ticker in two brokerages is
	// ordinary, with different cost bases. Archived holdings still occupy the
	// name (HoldingRepository.Create's contract), so the screen can offer
	// restore rather than show a bare 409.
	"holdings_account_id_name_key": domain.ErrHoldingNameTaken,
}

// translate converts driver errors into domain errors so nothing above the
// adapter layer ever sees pgx types.
func translate(err error, op string) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	case errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation:
		sentinel, named := uniqueConstraintErrors[pgErr.ConstraintName]
		if !named {
			// Mirrors ErrNotFound's translation: a caller-testable sentinel,
			// not a generic wrapped error, so usecase code can distinguish
			// "already exists" from any other failure with errors.Is. A
			// caller's own pre-check can race two concurrent creates past it
			// before either insert lands -- the UNIQUE constraint is the
			// backstop.
			sentinel = domain.ErrAlreadyExists
		}
		// op and pgErr.ConstraintName are folded into the message, not
		// discarded as a bare `return sentinel` would: for these
		// typed-sentinel errors, losing the diagnostic would be missed most
		// (every log line would read "already exists" with no way to tell
		// which collision). %w keeps it errors.Is-matchable, like the
		// default branch below.
		return fmt.Errorf("%s: constraint %q: %w", op, pgErr.ConstraintName, sentinel)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
