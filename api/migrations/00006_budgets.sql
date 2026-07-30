-- +goose Up

-- budgets is one household-month's plan. The row's existence IS "a budget is
-- set for this month" (spec decision 7): the empty state is one lookup, and a
-- month whose caps were all removed (a parent with zero lines) stays
-- distinguishable from a month never budgeted.
--
-- month is always the first of the month, the same convention
-- TransactionRepository.MonthTotals takes. Caps and expected income are in
-- the household's primary currency and deliberately carry no currency column:
-- a cap is a plan, not a transaction (spec decision 9). Changing the
-- household's primary currency changes what these numbers mean -- the same
-- accepted trade-off the accounts currency-change screen documents. Do not
-- "fix" this by adding a currency column.
CREATE TABLE budgets (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month                 date        NOT NULL,
    -- NULL means the household chose not to say, and hides the income and
    -- left-to-allocate cards. It never defaults to zero: zero is a claim.
    expected_income_minor bigint,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, month)
);

-- budget_lines is one category's cap inside one month's budget. ON DELETE
-- CASCADE from budgets: replacing a month's budget deletes and rewrites its
-- lines inside one transaction (BudgetRepository.Upsert). No cascade from
-- categories -- a category referenced by a line archives, never deletes,
-- the same reasoning accounts use.
CREATE TABLE budget_lines (
    id          uuid   PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id   uuid   NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    category_id uuid   NOT NULL REFERENCES categories(id),
    cap_minor   bigint NOT NULL CHECK (cap_minor >= 0),
    UNIQUE (budget_id, category_id)
);

-- +goose Down
DROP TABLE budget_lines;
DROP TABLE budgets;
