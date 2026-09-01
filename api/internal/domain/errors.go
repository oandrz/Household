// Package domain holds the business rules. Nothing here imports another
// internal package, a database driver, an HTTP library, or a clock.
package domain

import "errors"

var (
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrHouseholdLocked           = errors.New("household is locked")
	ErrLastOwner                 = errors.New("a household must keep at least one owner")
	ErrLimitedCannotHoldMarriage = errors.New("a limited member cannot hold the marriage capability")
	ErrUnknownCapability         = errors.New("unknown capability")
	ErrUnknownRole               = errors.New("unknown role")
	ErrCurrencyMismatch          = errors.New("cannot combine different currencies")
	ErrNotFound                  = errors.New("not found")
	ErrForbidden                 = errors.New("forbidden")
	ErrInviteExpired             = errors.New("invite has expired")
	ErrInviteAlreadyAccepted     = errors.New("invite has already been accepted")
	ErrInviteRequiresEmail       = errors.New("an invite requires an email address")
	ErrTokenExpired              = errors.New("token has expired or been used")
	ErrRateLimited               = errors.New("too many requests")

	// Added in the Task 6 fix round (see task-6-report.md, "Fix round 1").
	ErrAmountOverflow               = errors.New("amount overflows a signed 64-bit integer")
	ErrInvalidMoney                 = errors.New("money value is invalid")
	ErrOwnerMustHoldAllCapabilities = errors.New("an owner must hold every capability")

	ErrUnknownAccountType         = errors.New("unknown account type")
	ErrAccountNicknameRequired    = errors.New("an account nickname is required")
	ErrLiabilityBalanceNegative   = errors.New("a debt's balance is the amount owed and cannot be negative")
	ErrOpeningBalanceInFuture     = errors.New("an opening balance cannot be dated in the future")
	ErrAccountOwnerNotInHousehold = errors.New("that member is not in this household")

	// ErrUnknownCategoryKind is returned for a category kind this code did not
	// construct -- a database column holding something other than expense or
	// income.
	ErrUnknownCategoryKind = errors.New("unknown category kind")

	// The transaction sentinels. Each maps to a 422 with a field-specific code in
	// the HTTP layer (see errors.go there); none of them is an internal failure.
	ErrUnknownTransactionKind         = errors.New("unknown transaction kind")
	ErrTransactionDescriptionRequired = errors.New("transaction description is required")
	ErrTransactionAmountNotPositive   = errors.New("transaction amount must be positive")
	// ErrTransactionAccountsInvalid covers every wrong combination of the two
	// account fields: an expense with a destination, a transfer with one leg,
	// a transfer from an account to itself, or an account in another
	// household. They are one sentinel because the screen shows one message
	// next to the account pickers, and splitting them would tell an attacker
	// which ids exist elsewhere.
	ErrTransactionAccountsInvalid = errors.New("transaction accounts are not valid for its kind")
	ErrReceivedAmountRequired     = errors.New("a cross-currency transfer needs the amount received")
	ErrReceivedAmountNotAllowed   = errors.New("only a transfer can record an amount received")
	ErrCategoryKindMismatch       = errors.New("category does not match the transaction kind")

	// ErrAlreadyExists mirrors ErrNotFound: a row that must be unique
	// already exists. It exists so an adapter can translate a Postgres
	// unique-violation (SQLSTATE 23505) into something usecase code can
	// test with errors.Is, instead of a generic wrapped driver error. Added
	// in the Task 15 fix round (see task-15-report.md, "Fix round 2").
	ErrAlreadyExists = errors.New("already exists")

	// ErrCategoryNameTaken is UNIQUE (household_id, name) on categories,
	// translated the same way ErrAlreadyExists is for other tables. It
	// covers a collision with an archived row too -- an archived category
	// still occupies its unique key, so its name is not free to reuse.
	ErrCategoryNameTaken = errors.New("category name taken")

	// ErrCategoryNameRequired is CategoryService's Create/Rename guard, the
	// same shape as ErrAccountNicknameRequired: trim first, then refuse an
	// empty result rather than storing a category nobody could tell apart on
	// the Budget screen.
	ErrCategoryNameRequired = errors.New("a category name is required")

	// The budget sentinels. BudgetService.Save checks all three before
	// BudgetRepository.Upsert ever runs, following the per-field sentinel
	// convention above rather than a generic ErrValidation -- there is no
	// such sentinel in this codebase, deliberately, so every 422 the HTTP
	// layer returns can carry a field-specific code.
	ErrBudgetLineDuplicate  = errors.New("a budget line's category is repeated")
	ErrBudgetCapNegative    = errors.New("a budget cap cannot be negative")
	ErrBudgetIncomeNegative = errors.New("a budget's expected income cannot be negative")

	// ErrUnknownContributionSource is returned for a contribution source this
	// code did not construct -- a database column or request body holding
	// something other than manual, starting_balance, or budget_rollover.
	ErrUnknownContributionSource = errors.New("unknown contribution source")

	// ErrBudgetCategoryUnknown is BudgetRepository.Upsert's own
	// household-ownership check (validateLineCategories in the postgres
	// adapter) failing: a budget line names a category id that either does
	// not exist at all or belongs to a different household. Task 8's Save
	// deliberately does not pre-check this -- see its own doc comment -- so
	// this sentinel is what lets the HTTP layer turn that failure into a 422
	// instead of an unmapped 500.
	ErrBudgetCategoryUnknown = errors.New("a budget line's category does not belong to this household")

	// The goal sentinels. GoalService checks each before its repository call,
	// following the per-field convention above rather than a generic
	// validation error, so every 422 carries a field-specific code.
	ErrGoalNameRequired           = errors.New("a goal name is required")
	ErrGoalNameTaken              = errors.New("goal name taken")
	ErrGoalTargetNotPositive      = errors.New("a goal's target must be positive")
	ErrGoalPlannedMonthlyNegative = errors.New("a goal's planned monthly amount cannot be negative")
	ErrGoalCurrencyImmutable      = errors.New("a goal's currency cannot be changed")
	ErrGoalArchived               = errors.New("that goal is archived")
	ErrContributionAmountZero     = errors.New("a contribution cannot be zero")

	// The rollover sentinels. Each is a refusal BudgetService.RollOver makes
	// before anything is written; ErrRolloverAlreadyDone can also arrive from
	// the repository's own conditional UPDATE losing a race.
	ErrRolloverMonthOpen        = errors.New("only a closed month can be rolled over")
	ErrRolloverAlreadyDone      = errors.New("that month has already been rolled over")
	ErrRolloverNothingUnspent   = errors.New("that month has nothing unspent to roll over")
	ErrRolloverCurrencyMismatch = errors.New("only a goal in the household's primary currency can receive a rollover")

	// Bills. ErrUnknownCadence is returned for a cadence this code did not
	// construct -- it arrives from a database column and from a request body,
	// so both layers refuse it, the same rule ParseTransactionKind and
	// ParseContributionSource already follow.
	ErrUnknownCadence        = errors.New("unknown bill cadence")
	ErrBillNameRequired      = errors.New("a bill name is required")
	ErrBillAmountNotPositive = errors.New("a bill amount must be positive")
	// ErrBillNameTaken is UNIQUE (household_id, name) on bills, translated the
	// same way ErrCategoryNameTaken and ErrGoalNameTaken are. An archived bill
	// still holds its name, so the HTTP layer offers restore rather than a
	// bare 409.
	ErrBillNameTaken = errors.New("bill name taken")
	// ErrBillCurrencyImmutable is BillService.Update's own guard: a bill's
	// amount is stored in its pay-from account's currency (BillRecord's own
	// comment in ports.go), so re-pointing PayFromAccountID at an account in
	// a different currency would silently reinterpret every past figure.
	// Added in Task 6 (see task-6-report.md).
	ErrBillCurrencyImmutable = errors.New("a bill's currency cannot be changed")

	// ErrInvalidMood is returned when a mood outside 1..5 arrives from a
	// request body or a database column. Nothing defaults an invalid mood to
	// a valid one: a retro with no mood is a real state (NULL), and silently
	// rounding 7 to 5 would invent a feeling nobody recorded.
	ErrInvalidMood = errors.New("a mood must be between 1 and 5")

	// ErrRetroChanged is returned when a retro update carries a version older
	// than the stored one -- the other partner saved while this one was
	// typing. The write is refused, never merged: silently overwriting the
	// other person's paragraph is the failure this guard exists to prevent.
	ErrRetroChanged = errors.New("this retro changed while you were editing it")

	// ErrRetroNothingToStart is returned when both candidate months -- the
	// current one and the previous one -- already have a retro, so there is
	// nothing left for "Start retro" to create (domain.StartableMonth's own
	// `ok == false` case). The HTTP layer maps this to 409 (Task 8).
	ErrRetroNothingToStart = errors.New("both candidate months already have a retro")

	// ErrRetroActionBodyRequired is returned when an action's body is empty
	// or whitespace-only. A blank row on the retro detail is indistinguishable
	// from a rendering bug, which is why it is refused rather than trimmed to
	// empty and saved.
	ErrRetroActionBodyRequired = errors.New("a retro action needs a body")

	// ErrVisionThemeRequired is a save with no theme. The empty vision GET
	// returns for a year never set is allowed to have none; a save is not.
	ErrVisionThemeRequired = errors.New("a vision needs a theme")

	ErrVisionThemeTooLong = errors.New("a vision theme is too long")

	ErrVisionDescriptionTooLong = errors.New("a vision description is too long")

	ErrVisionYearOutOfRange = errors.New("a year must be between 1900 and 2200")

	ErrVisionPillarNameRequired = errors.New("a pillar needs a name")

	ErrVisionMeasureLabelRequired = errors.New("a measure needs a label")

	ErrVisionMeasureTargetNotPositive = errors.New("a measure target must be positive")

	ErrVisionMeasureCurrentNegative = errors.New("a measure's current value cannot be negative")

	// ErrVisionMeasureAmbiguous covers every shape that is neither cleanly typed
	// nor cleanly linked -- both at once, neither at all, or an unrecognised
	// kind. One error rather than three: from the editor's point of view they are
	// the same mistake, and the database's measure_is_typed_or_linked refuses the
	// same set.
	ErrVisionMeasureAmbiguous = errors.New("a measure is either typed or linked to a goal, never both")

	ErrVisionMeasureGoalRequired = errors.New("a linked measure needs a goal")

	ErrVisionMilestoneTitleRequired = errors.New("a milestone needs a title")

	ErrVisionTooManyPillars = errors.New("too many pillars")

	ErrVisionTooManyMeasures = errors.New("too many measures on one pillar")

	ErrVisionTooManyMilestones = errors.New("too many milestones")

	// ErrVisionChanged is the optimistic-concurrency refusal, the twin of
	// ErrRetroChanged. It also covers the first save: two owners who both read an
	// unset year both hold version 0, and the second one must be told rather than
	// silently overwriting a whole year of pillars.
	ErrVisionChanged = errors.New("this vision changed while you were editing it")

	// ErrVisionGoalUnknown is a measure naming a goal that is not this
	// household's. Indistinguishable from a goal that does not exist, the scoping
	// rule every repository here already follows.
	ErrVisionGoalUnknown = errors.New("a measure's goal does not belong to this household")

	// ErrUnknownFlag is returned for a feature-flag key this build does not
	// define -- from a request, or from an override row that outlived the
	// const that named it.
	ErrUnknownFlag = errors.New("unknown feature flag")

	// ErrAdminLocked is the admin surface's own lockout, evaluated over
	// admin_reauth_attempts. It is deliberately separate from
	// ErrHouseholdLocked: locking the operator out of /admin must never lock
	// their household out of the product.
	ErrAdminLocked = errors.New("admin re-authentication is locked")
)
