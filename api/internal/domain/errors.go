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
	ErrTokenExpired              = errors.New("token has expired or been used")
	ErrRateLimited               = errors.New("too many requests")

	// Added in the Task 6 fix round (see task-6-report.md, "Fix round 1").
	ErrAmountOverflow               = errors.New("amount overflows a signed 64-bit integer")
	ErrInvalidMoney                 = errors.New("money value is invalid")
	ErrOwnerMustHoldAllCapabilities = errors.New("an owner must hold every capability")
)
