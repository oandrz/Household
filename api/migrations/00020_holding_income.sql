-- +goose Up

-- holding_income is every payment a holding made to the household, and every
-- charge made against it, that is NOT a trade.
--
-- It is a separate table rather than a third holding_events.kind, and the
-- reason is arithmetic rather than tidiness: a dividend changes neither the
-- quantity held nor what that quantity cost, so folding it through the
-- average-cost pool would make every disposal after it realise the wrong
-- number. 00019's own comment said this, and this table is where the promise
-- comes due.
--
-- Brokerage on a trade does NOT belong here. An acquisition already records
-- the whole amount that left the bank and a disposal the whole amount that
-- arrived, so commission is inside the cost basis and the proceeds already.
-- Only a charge that buys nothing -- custody, platform, vault storage -- needs
-- a row.
CREATE TABLE holding_income (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    holding_id    uuid        NOT NULL REFERENCES holdings(id),
    household_id  uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Both kinds are stored POSITIVE and the report subtracts the fees. A
    -- negative amount would be the only negative money in this schema, and one
    -- exception is how a rule stops being a rule. The Go parser fails closed
    -- on this column too, the house pattern.
    kind          text        NOT NULL CHECK (kind IN ('income', 'fee')),
    -- Strictly greater than zero, unlike holding_events.amount_minor which
    -- allows zero because it is the QUANTITY that must be positive there. A
    -- zero row here would record that nothing happened.
    amount_minor  bigint      NOT NULL CHECK (amount_minor > 0),
    -- The same payment in the household's primary currency, supplied by the
    -- owner rather than derived from a rate, with its code stored beside it --
    -- identical contract and identical reasoning to holding_events; see that
    -- table's comment in 00019.
    primary_amount_minor bigint,
    primary_currency     char(3),
    -- The day the money moved, not the day it was typed. The report buckets by
    -- it, so a dividend backfilled in July still counts in the quarter it was
    -- paid.
    received_on   date        NOT NULL,
    note          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT income_primary_amount_is_whole CHECK (
        (primary_amount_minor IS NULL) = (primary_currency IS NULL)
    ),
    CONSTRAINT income_primary_amount_not_negative CHECK (
        primary_amount_minor IS NULL OR primary_amount_minor >= 0
    )
);

-- THERE IS NO ORDERING CONTRACT HERE, and holding_events_fold_idx has no
-- sibling on this table on purpose. Income is summed over a period and
-- addition is commutative, so no order changes the answer -- unlike the event
-- fold, where two same-day rows in the other order realise a different gain.
-- An index shaped like the fold's would invite a reader to hunt for a fold
-- that does not exist.
--
-- This index is for the read the report actually does: every income row a
-- household has, grouped by holding, bucketed by date.
CREATE INDEX holding_income_household_idx
    ON holding_income (household_id, holding_id, received_on);

-- +goose Down
DROP TABLE holding_income;
