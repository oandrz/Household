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
// sentinel its violation means. translate matches by name, not only by
// SQLSTATE 23505, so a future unique key on the same table -- or any other
// 23505 whose message happens to mention it -- cannot masquerade as one of
// these collisions: an unlisted name falls through to ErrAlreadyExists. A new
// named collision is one entry here, not a new case in translate.
//
// Most names are Postgres's default for an unnamed table constraint,
// "<table>_<columns>_key"; the two indexes were named in their migrations.
var uniqueConstraintErrors = map[string]error{
	// categories' UNIQUE (household_id, name), 00005_transactions.sql.
	// CategoryRepository's own contract (usecase/ports.go) wants a sentinel
	// specific to this constraint, not the generic ErrAlreadyExists: Create
	// and Rename both hit it on a name collision, archived rows included,
	// since archived_at is not part of the unique key.
	"categories_household_id_name_key": domain.ErrCategoryNameTaken,

	// goals' UNIQUE (household_id, name), 00007_goals.sql. GoalRepository's
	// Create and Update both hit this on a name collision, archived rows
	// included -- the same archived-still-occupies-its-key rule categories
	// follow.
	"goals_household_id_name_key": domain.ErrGoalNameTaken,

	// goal_contributions' partial unique index on (household_id,
	// source_budget_month) WHERE source = 'budget_rollover', 00007_goals.sql.
	// BudgetRepo.RollOverToGoal's own doc comment: a concurrent pair that both
	// reach the INSERT must not surface as a raw 23505. StampBudgetRollover's
	// conditional UPDATE is the first line of defence and normally catches
	// this before the INSERT is ever attempted; this index is what makes a
	// future code path that forgets that UPDATE fail safely instead of
	// silently.
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

	// agreement_sections' UNIQUE (household_id, name), 00015_agreements.sql
	// (decision 19). AgreementRepository.CreateSection's contract: the unique
	// index decides the collision, never a pre-read, and the screen has to be
	// able to say "you already have a section called that". Sections are never
	// deleted, so a name is never freed once taken.
	"agreement_sections_household_id_name_key": domain.ErrAgreementSectionNameTaken,

	// holdings' UNIQUE (account_id, name), 00019_holdings.sql. Scoped to the
	// ACCOUNT, not the household: holding the same ticker in two brokerages is
	// ordinary, and they are genuinely different positions with different cost
	// bases. HoldingRepository.Create's contract: archived holdings still
	// occupy the name, and the screen has to be able to offer restore rather
	// than show a bare 409.
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
			// Mirrors ErrNotFound's translation: a caller-testable domain
			// sentinel rather than a generic wrapped driver error, so
			// usecase-level code can distinguish "this already exists" from
			// any other failure with errors.Is. Task 15's CreateSpace is the
			// first caller: its own pre-check (list, then compare keys) closes
			// the common case, but two concurrent creates deriving the same
			// key can both pass that check before either insert lands, and the
			// database's UNIQUE (household_id, key) constraint is the
			// authoritative backstop for that race.
			sentinel = domain.ErrAlreadyExists
		}
		// op and pgErr.ConstraintName are folded into the message -- not just
		// discarded the way a bare `return sentinel` would -- because these are
		// the errors with a typed sentinel a caller can match against, which
		// makes them exactly the case where losing the diagnostic (which
		// operation, which constraint) would be missed most: every log line
		// would otherwise read "already exists" with no way to tell
		// CreateSpace's key collision apart from CreateUser's email collision.
		// %w keeps it errors.Is-matchable despite the wrapping, exactly as the
		// default branch below does for every other error.
		return fmt.Errorf("%s: constraint %q: %w", op, pgErr.ConstraintName, sentinel)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
