-- +goose Up

-- goals is one household's savings target. Unlike budgets (00006), a goal
-- carries an explicit currency: a budget is one month's plan and a
-- primary-currency change restating it was an accepted cost, while a goal
-- accumulates for years and the same silence would restate a multi-year total
-- and every contribution behind it. accounts stores currency per row for this
-- exact reason. Do not "fix" this by dropping the column.
--
-- target_month is NULL for a goal with no target date -- the design's own
-- Emergency fund ("6 months expenses", no date). A dateless goal shows
-- progress and carries no on-track status at all, because the status rule
-- divides by months left and there is no honest number to divide by.
CREATE TABLE goals (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id          uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                  text        NOT NULL,
    target_amount_minor   bigint      NOT NULL CHECK (target_amount_minor > 0),
    currency              char(3)     NOT NULL,
    -- Always the first of the month, the same convention budgets.month and
    -- TransactionRepository.MonthTotals take.
    target_month          date,
    planned_monthly_minor bigint      NOT NULL CHECK (planned_monthly_minor >= 0),
    -- A goal is archived, never deleted: contributions reference it, and a
    -- rolled-over budget month names it. The accounts precedent, for the
    -- accounts reason.
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- An archived goal still occupies its name, exactly as an archived
    -- category does. A collision with one offers restore rather than a bare
    -- 409 (see the HTTP task).
    UNIQUE (household_id, name)
);

-- goal_contributions is what a goal's progress is made of. A contribution
-- moves no real money: a goal earmarks, it does not hold, so goal progress and
-- account balances are independent figures and nothing reconciles them (spec
-- decision 1). Do not "fix" this by joining transactions -- that is the
-- larger, later feature the spec's own decision 1 rejected.
--
-- The row carries no currency: it is its goal's, by construction.
CREATE TABLE goal_contributions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id      uuid        NOT NULL REFERENCES goals(id),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Non-zero rather than positive: a genuine correction downward (a goal the
    -- household raided) is a negative row, while zero is meaningless.
    amount_minor bigint      NOT NULL CHECK (amount_minor <> 0),
    occurred_on  date        NOT NULL,
    note         text        NOT NULL DEFAULT '',
    -- A new source needs a migration. That is deliberate: the Go parser fails
    -- closed on this column too, and both layers refusing an unknown value is
    -- the house pattern (transactions.kind carries the same CHECK).
    source       text        NOT NULL
                             CHECK (source IN ('manual', 'starting_balance', 'budget_rollover')),
    -- Set only on a budget rollover: which month's unspent money this is.
    -- Deleting the row clears that month's stamp on budgets, and finding the
    -- month from a note string would be guesswork.
    source_budget_month date,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT budget_month_is_a_rollover_thing CHECK (
        source = 'budget_rollover' OR source_budget_month IS NULL
    ),
    CONSTRAINT rollover_names_its_month CHECK (
        source <> 'budget_rollover' OR source_budget_month IS NOT NULL
    )
);

CREATE INDEX goal_contributions_goal_idx
    ON goal_contributions (goal_id);

-- The "actual this month" figure walks one household's contributions by date.
CREATE INDEX goal_contributions_household_date_idx
    ON goal_contributions (household_id, occurred_on);

-- Belt and braces beside the conditional UPDATE in RollOverToGoal: even a
-- future code path that forgets the stamp cannot write two rollovers for one
-- household-month.
CREATE UNIQUE INDEX goal_contributions_one_rollover_per_month
    ON goal_contributions (household_id, source_budget_month)
    WHERE source = 'budget_rollover';

-- A month can be rolled over exactly once, and the record survives the goal
-- being archived -- so no ON DELETE clause here: goals are never deleted.
ALTER TABLE budgets
    ADD COLUMN rolled_over_at   timestamptz,
    ADD COLUMN rollover_goal_id uuid REFERENCES goals(id);

ALTER TABLE budgets
    ADD CONSTRAINT rollover_stamp_is_whole CHECK (
        (rolled_over_at IS NULL AND rollover_goal_id IS NULL)
     OR (rolled_over_at IS NOT NULL AND rollover_goal_id IS NOT NULL)
    );

-- +goose Down
ALTER TABLE budgets DROP CONSTRAINT rollover_stamp_is_whole;
ALTER TABLE budgets DROP COLUMN rollover_goal_id;
ALTER TABLE budgets DROP COLUMN rolled_over_at;
DROP TABLE goal_contributions;
DROP TABLE goals;
