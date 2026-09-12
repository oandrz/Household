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

	// ErrQuantityNegative is NewQuantity's refusal. A holding's quantity is how
	// much of a thing is held, so negative is not a smaller amount -- it is a
	// different claim, and one no screen in this product can render. A disposal
	// is recorded as its own event with its own positive quantity, never as a
	// negative holding.
	ErrQuantityNegative = errors.New("a quantity cannot be negative")
	// ErrInvalidQuantity is ParseQuantity's refusal -- a quantity that is not a
	// number, carries a sign, or is finer than a billionth. Separate from
	// ErrInvalidMoney because a quantity is not money and a screen's message
	// for one is wrong for the other.
	ErrInvalidQuantity = errors.New("that is not a quantity")

	// ErrProrateWholeNotPositive is Money.Prorate's refusal to divide a cost
	// pool by an empty holding. Returning zero instead would report that a
	// disposal cost nothing, which reads on screen as pure profit.
	ErrProrateWholeNotPositive = errors.New("cannot prorate across a zero quantity")
	// ErrProratePartExceedsWhole keeps Prorate's own refusal separate from the
	// holding fold's ErrHoldingOversold. They fire on the same shape but mean
	// different things -- one is "this proportion is not a proportion", the
	// other is "this household does not own that much" -- and collapsing them
	// into one error made each guard untestable, because either alone still
	// produced the error the test looked for.
	ErrProratePartExceedsWhole = errors.New("cannot prorate more than the whole")

	// The holding errors. ErrHoldingOversold is the fold refusing to sell more
	// than is held: the alternative is a negative quantity, which NewQuantity
	// already refuses, and a position no screen can render.
	ErrUnknownInstrumentKind           = errors.New("unknown instrument kind")
	ErrUnknownHoldingEventKind         = errors.New("unknown holding event kind")
	ErrHoldingOversold                 = errors.New("cannot dispose of more than is held")
	ErrHoldingEventQuantityNotPositive = errors.New("a holding event must move a positive quantity")
	// The two sides of the cross-currency rule, mirroring
	// ErrReceivedAmountRequired and ErrReceivedAmountNotAllowed on transfers:
	// the primary-currency figure is required exactly when it is a different
	// number, and refused when it would duplicate the native one.
	ErrHoldingPrimaryAmountRequired   = errors.New("a holding not in the primary currency needs its primary-currency amount")
	ErrHoldingPrimaryAmountNotAllowed = errors.New("a holding already in the primary currency must not carry a second amount")
	// ErrHoldingNameTaken is the UNIQUE (account_id, name) collision, scoped to
	// the account rather than the household because holding the same ticker in
	// two brokerages is ordinary. Archived holdings still occupy their name, so
	// a collision with one offers restore rather than a bare 409 -- the goals
	// and categories rule.
	ErrHoldingNameTaken    = errors.New("a holding with that name already exists in this account")
	ErrHoldingNameRequired = errors.New("a holding name is required")
	// ErrHoldingDateInFuture is the sibling of ErrOpeningBalanceInFuture, and
	// matters more here: latest-price lookups order by as_of, so a price
	// mistyped as 2030 outranks every real one forever and pins the holding's
	// market value to a figure nobody can explain. Today is not the future --
	// this project has shipped an off-by-one at exactly that boundary three
	// times (see LEARNING's timezone pattern), so the comparison is on the
	// calendar day, not the instant.
	ErrHoldingDateInFuture = errors.New("that date is in the future")
	// ErrHoldingAccountNotInvestment fails closed on the account's type rather
	// than trusting a screen to have offered only the right accounts. The
	// sibling rule lives in AccountService: an account holding live holdings
	// cannot have its type changed out from under them.
	ErrHoldingAccountNotInvestment = errors.New("a holding belongs in an investment account")

	// The reporting periods. ErrPeriodIndexOutOfRange is a fifth quarter or a
	// zeroth half -- refused at construction so that no arithmetic downstream
	// has to wonder whether the period it holds is real.
	ErrUnknownPeriodKind     = errors.New("unknown period kind")
	ErrPeriodIndexOutOfRange = errors.New("that period does not exist in a year")
	ErrPeriodCountOutOfRange = errors.New("a report covers at least one period")

	// Holding income. A fee is stored positive and subtracted when a period is
	// summed, so a zero row is the only meaningless one -- there is nothing to
	// record when nothing changed hands.
	ErrUnknownIncomeKind              = errors.New("unknown holding income kind")
	ErrHoldingIncomeAmountNotPositive = errors.New("a holding income row must move a positive amount")

	// ErrPrimaryCurrencyHeldByHoldings stops a household changing the currency
	// it keeps its books in while it holds investments. Every holding event
	// records its cost in the currency that was primary WHEN IT WAS WRITTEN,
	// and nothing in the data can re-express an old figure under a new one --
	// so the change would strand the holding rather than restate it.
	ErrPrimaryCurrencyHeldByHoldings = errors.New("the primary currency cannot change while the household holds investments")
	// ErrHoldingArchived is the same rule an archived goal follows: restoring
	// is a deliberate act, and writing to an archived holding would silently
	// un-retire a position the household said was finished.
	ErrHoldingArchived = errors.New("that holding is archived")
	// ErrAccountHasHoldings refuses a type change on an account that still
	// holds something. usecase/account.go patches Type freely, so without this
	// a cash account could end up holding 300g of gold.
	ErrAccountHasHoldings = errors.New("that account holds investments and cannot change type")

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

	// ErrAPITokenNameInvalid and ErrAPITokenLifetimeInvalid are the two
	// refusals of POST /auth/tokens: a blank or overlong name, and a
	// lifetime outside (0, MaxAPITokenLifetime]. Both 422.
	ErrAPITokenNameInvalid     = errors.New("api token name is not valid")
	ErrAPITokenLifetimeInvalid = errors.New("api token lifetime is not valid")

	// ErrIdempotencyKeyInvalid is ValidateIdempotencyKey's refusal: a
	// caller-supplied key of the wrong shape. 422, never a silent "no key".
	ErrIdempotencyKeyInvalid = errors.New("idempotency key is not valid")
	// ErrIdempotencyKeyInUse is the adapter's translation of the
	// transactions_household_idempotency_key unique violation: this
	// household already has a transaction with this key. TransactionService
	// turns it into either a replay or ErrIdempotencyKeyReused; it never
	// reaches a handler.
	ErrIdempotencyKeyInUse = errors.New("idempotency key already in use")
	// ErrIdempotencyKeyReused is a repeated create whose key matches a stored
	// row but whose fields do not: the caller reused a key for a different
	// transaction. 409. Handing back the stored row instead would tell the
	// caller its retry "worked" for something it never asked for.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused for a different transaction")

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

	// --- Agreements ---

	// ErrAgreementsNeedTwoOwners is decision 1's gate. It sits here, in this
	// "--- Agreements ---" block after ErrAdminLocked, grouped with the
	// feature's other sentinels below it rather than beside ErrLastOwner
	// (208 lines up) — even though it is, like ErrLastOwner, a fact about
	// the owner set rather than part of any port's contract. Every write
	// refuses with it while a household has fewer than MinAgreementOwners
	// owners.
	ErrAgreementsNeedTwoOwners = errors.New("agreements need at least two owners")

	// ErrAgreementChanged is a target that moved, went, or no longer reads the proposal's previous_body.
	// Deliberately not ErrNotFound: "it vanished" and "someone changed it" are different things to be told,
	// and ErrNotFound on these routes means the proposal row itself.
	ErrAgreementChanged = errors.New("the agreement this proposal targets has changed")

	// ErrAgreementNotOpen is a sign, park or withdraw against a resolved proposal -- ordinarily the last
	// signer double-clicking Agree. It means "reload, this was settled", not "try again", and it pairs with
	// AgreementProposalStatus.IsOpen. The wire code it maps to is AGREEMENT_PROPOSAL_RESOLVED, deliberately
	// worded from the caller's side rather than this sentinel's.
	ErrAgreementNotOpen = errors.New("this proposal is no longer open")

	// Sections are never deleted, so a name is never freed again (decision 19).
	ErrAgreementSectionNameTaken    = errors.New("that section name is already used")
	ErrAgreementSectionNameRequired = errors.New("a section needs a name")
	ErrAgreementSectionNameTooLong  = errors.New("a section name is too long")

	ErrAgreementBodyRequired = errors.New("an agreement needs a body")
	ErrAgreementBodyTooLong  = errors.New("an agreement body is too long")

	// The two notes get a sentinel each rather than sharing one, so every 422 can name the field the screen
	// has to highlight -- and so that MaxAgreementParkNoteLen quietly becoming an alias of MaxAgreementNoteLen
	// is visible somewhere. AgreementService.Park's test is that somewhere; no domain assertion can see it.
	ErrAgreementNoteTooLong     = errors.New("a proposal note is too long")
	ErrAgreementParkNoteTooLong = errors.New("a discussion note is too long")

	// ErrAgreementProposalShapeInvalid covers every wrong combination of the four content fields -- an add
	// with no section, an edit with no target, a remove with no previous body -- as one sentinel, the way
	// ErrTransactionAccountsInvalid does: the modal sends one of three complete shapes, so a mismatch is a
	// hand-built request, and four codes would only tell it which field to try next.
	ErrAgreementProposalShapeInvalid = errors.New("this proposal's fields do not match its kind")

	// Separate from the shape refusal, because there the shape is fine and the screen says something
	// different: the version number is a promise that something happened.
	ErrAgreementEditUnchanged = errors.New("an edit must change the wording")

	// Neither of these gets a MapDomainError case (Task 8). A bad kind in a request body is answered 422 by
	// the handler's own parser (decision 21), so both can only reach the mapper from a database column --
	// where a logged 500 is the right answer to an impossible row.
	ErrUnknownAgreementProposalKind   = errors.New("unknown agreement proposal kind")
	ErrUnknownAgreementProposalStatus = errors.New("unknown agreement proposal status")

	// --- Telegram account linking ---

	// ErrTelegramChatTaken is a chat already bound to a different Hearth
	// user. Named separately from ErrTelegramAlreadyLinked because the two
	// need different sentences: one is "that phone belongs to someone else",
	// the other is "you already have a phone".
	ErrTelegramChatTaken = errors.New("that telegram chat is connected to another account")

	// ErrTelegramAlreadyLinked is this user already having a chat. One chat
	// per user is a database constraint; this is how it reads to a person.
	ErrTelegramAlreadyLinked = errors.New("this account already has a telegram chat")

	// ErrTelegramLinkNotPending covers a confirm before any chat redeemed
	// the link, and a confirm after it expired. The two are one error
	// because the panel's next instruction is the same for both: start again.
	ErrTelegramLinkNotPending = errors.New("no chat has opened this link")

	// ErrTelegramUnlinkWouldLockOut is a disconnect refused because the
	// account has no email address. GetUserByEmail is WHERE email = $1 and
	// NULL never matches a parameter, so a Telegram-only account that
	// disconnects has no magic link, no password reset and no adminctl path
	// back in -- only make psql by hand.
	ErrTelegramUnlinkWouldLockOut = errors.New("this account has no email address to sign in with")

	// ErrTelegramMintsRateLimited bounds how many link attempts one member
	// can start in an hour. Table growth, not a security control.
	ErrTelegramMintsRateLimited = errors.New("too many telegram link attempts")
)
