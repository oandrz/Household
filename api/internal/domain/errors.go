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
)
