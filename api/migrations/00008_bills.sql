-- +goose Up

-- bills is one household's recurring fixed costs. Unlike goals (00007), a bill
-- carries no currency column: it is denominated in its pay-from account's
-- currency, because TransactionService.Create already forces an expense's
-- currency to its from-account's (usecase/transaction.go:232). A currency
-- here would be overwritten the moment a payment wrote its transaction, and
-- the two would disagree in the meantime. Do not "fix" this by adding one.
CREATE TABLE bills (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                  text        NOT NULL,
    amount_minor          bigint      NOT NULL CHECK (amount_minor > 0),
    cadence               text        NOT NULL
                                      CHECK (cadence IN ('one_off','monthly','quarterly','yearly')),
    -- NULL only for a settled one-off: paid, and with no next date. A settled
    -- one-off is NOT auto-archived -- that would hide a record the household
    -- may still want to see.
    next_due              date,
    -- The day of the month the household actually chose, kept apart from
    -- next_due because clamping is lossy. 31 Jan clamps to 28 Feb; advancing
    -- from 28 would give 28 Mar and the bill would have silently moved off the
    -- 31st forever. Each advance clamps THIS value to the destination month,
    -- so 31 Jan -> 28 Feb -> 31 Mar. Set from next_due at create, reset when
    -- next_due is patched.
    due_anchor_day        smallint    NOT NULL CHECK (due_anchor_day BETWEEN 1 AND 31),
    category_id           uuid        REFERENCES categories(id) ON DELETE SET NULL,
    -- NOT NULL: it supplies the currency as well as the account the expense
    -- leaves. No ON DELETE clause -- accounts are never deleted, only
    -- archived, the same reason goals carries none.
    pay_from_account_id   uuid        NOT NULL REFERENCES accounts(id),
    -- Optional. Its absence is why BudgetByPerson grows an Unattributed row
    -- (spec decision 8) rather than silently dropping the spend, which is what
    -- usecase/budget.go:252 does today for any transaction with no payer.
    paid_by_membership_id uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    -- Display only. Nothing in this product pays a bill by itself: there is no
    -- scheduler anywhere in this codebase, and Budget decision 1 and Goals
    -- decision 4 both refused to invent one. This flag drives a badge, a
    -- count, and the wording of the overdue state.
    autopay               boolean     NOT NULL DEFAULT false,
    -- Set by the household, never inferred from the category: categories are
    -- editable and shared with transactions and budgets, so renaming one would
    -- silently empty the subscriptions panel.
    is_subscription       boolean     NOT NULL DEFAULT false,
    -- A bill is archived, never deleted: bill_payments references it. The
    -- accounts/categories/goals precedent, for the same reason.
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- An archived bill still occupies its name, exactly as an archived goal or
    -- category does. A collision with one offers restore rather than a bare
    -- 409 (see the HTTP task).
    UNIQUE (household_id, name),
    -- A settled one-off is the only bill without a next date. Anything else
    -- with a NULL next_due is a bug that would vanish from every list.
    CONSTRAINT only_a_one_off_has_no_next_due CHECK (
        next_due IS NOT NULL OR cadence = 'one_off'
    )
);

CREATE INDEX bills_household_idx ON bills (household_id) WHERE archived_at IS NULL;

-- bill_payments is the record of what was actually paid, and the only place a
-- past occurrence exists at all: bills carries one date forward, never a
-- history. amount_minor is what was paid, which may differ from the bill's own
-- amount -- utilities vary, and the Mark-paid modal lets the figure be edited.
CREATE TABLE bill_payments (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_id        uuid        NOT NULL REFERENCES bills(id),
    household_id   uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Which occurrence this settles -- the bill's next_due at the moment of
    -- paying, NOT the date it was paid on. Every month-scoped figure keys off
    -- this column, so a bill paid late still counts against the month it was
    -- due in.
    due_on         date        NOT NULL,
    paid_on        date        NOT NULL,
    amount_minor   bigint      NOT NULL CHECK (amount_minor > 0),
    -- SET NULL rather than CASCADE: deleting the expense from the Transactions
    -- page must not erase the household's record that the bill was paid. The
    -- payment row's own amount_minor is what survives.
    transaction_id uuid        REFERENCES transactions(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- Belt and braces beside BillService's own check: a double-clicked Mark
    -- paid cannot write two payments for one occurrence.
    UNIQUE (bill_id, due_on)
);

-- The month figures walk one household's payments by the date they were DUE,
-- not the date they were paid.
CREATE INDEX bill_payments_household_due_idx ON bill_payments (household_id, due_on);

-- +goose Down
DROP TABLE bill_payments;
DROP TABLE bills;
