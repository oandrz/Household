-- +goose Up

-- holdings is what a household actually owns inside an investment account, as
-- opposed to what that account's balance says it is worth. The two are
-- different questions: a balance moves both when the market moves and when the
-- household puts more money in, and telling those apart is the whole reason
-- this table exists.
--
-- NOTHING IN THIS MIGRATION TOUCHES accounts, net worth or the ledger. A
-- holding is invisible to ListAccounts and to the twelve-month trend by
-- design (milestone 1 of the portfolio plan); making the account's balance
-- read from holdings is milestone 3's work and needs its own decision about
-- whether an investment account also carries uninvested cash. Wiring it up
-- here, in passing, is how that decision would get made badly.
--
-- currency is per row, not inherited from the household, for the reason goals
-- carry theirs (00007_goals.sql): a holding accumulates for years, and letting
-- a primary-currency change restate its cost would restate every event behind
-- it. A US stock inside a Singapore brokerage is the ordinary case here, not
-- the exotic one.
CREATE TABLE holdings (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- No ON DELETE clause: accounts are archived, never deleted (00004), and a
    -- holding whose account vanished would be unreachable money.
    account_id   uuid        NOT NULL REFERENCES accounts(id),
    name         text        NOT NULL,
    -- The Go parser fails closed on this column too, and both layers refusing
    -- an unknown value is the house pattern (transactions.kind, goal
    -- contributions.source). A new instrument kind needs a migration, and
    -- that is deliberate.
    instrument   text        NOT NULL
                             CHECK (instrument IN ('stock', 'gold', 'other')),
    -- What one of it is called: "share", "gram", "unit". A label for a screen;
    -- nothing computes with it.
    unit         text        NOT NULL,
    currency     char(3)     NOT NULL,
    -- A holding is archived, never deleted: its events and valuations
    -- reference it, and a sold-out position is still part of the year's
    -- realised profit. The goals and accounts precedent, for their reason.
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- Scoped to the ACCOUNT, not the household: holding the same ticker in two
    -- brokerages is ordinary, and they are genuinely different positions with
    -- different cost bases. Do not "fix" this to (household_id, name) -- that
    -- would make the ordinary case unrepresentable.
    --
    -- An archived holding still occupies its name, exactly as an archived goal
    -- or category does, so a collision with one offers restore rather than a
    -- bare 409.
    UNIQUE (account_id, name)
);

CREATE INDEX holdings_household_idx ON holdings (household_id);

-- holding_events is every acquisition and disposal. Income (a dividend) is
-- deliberately NOT one of them: it changes neither what is held nor what it
-- cost, so folding it in here would corrupt the average cost. It arrives with
-- the period report, on its own footing.
--
-- amount_minor is the WHOLE event -- what the lot cost, or what the sale
-- brought in -- not a unit price. That is the figure on the owner's bank
-- statement, and a unit price divided out of it would round before anything
-- else got the chance.
--
-- quantity_nano is billionths of a unit, matching domain.QuantityScale. A
-- quantity is genuinely fractional (300.5 grams, half a share) and float64
-- never appears in this product's monetary path, so it is an integer here for
-- the same reason money is. CHANGING THE SCALE SILENTLY RESTATES EVERY ROW:
-- it would need a migration that rewrites this column, not a new constant.
CREATE TABLE holding_events (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    holding_id    uuid        NOT NULL REFERENCES holdings(id),
    household_id  uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    kind          text        NOT NULL CHECK (kind IN ('acquisition', 'disposal')),
    quantity_nano bigint      NOT NULL CHECK (quantity_nano > 0),
    amount_minor  bigint      NOT NULL CHECK (amount_minor >= 0),
    -- The same event in the household's primary currency, supplied by the
    -- owner rather than derived from a rate: this product has no dated rate
    -- source (adapter/fx/static.go holds one pair), and what the owner
    -- actually knows is the amount that left their bank, not the ratio.
    --
    -- The currency is stored beside the amount because, unlike a transfer's
    -- received_amount, there is no account to join it from. A household that
    -- later changes its primary currency would otherwise leave these figures
    -- silently meaning something they no longer mean; with the code stored,
    -- a report can blank-with-a-reason instead, which is this product's rule
    -- for a figure it cannot honestly compute.
    primary_amount_minor bigint,
    primary_currency     char(3),
    occurred_on   date        NOT NULL,
    note          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT primary_amount_is_whole CHECK (
        (primary_amount_minor IS NULL) = (primary_currency IS NULL)
    ),
    CONSTRAINT primary_amount_not_negative CHECK (
        primary_amount_minor IS NULL OR primary_amount_minor >= 0
    )
);

-- The fold's ordering contract, and the reason this index exists in exactly
-- this shape. occurred_on is a DATE, so buying and selling the same morning
-- share one, and the order between two same-day events changes the realised
-- gain: on identical events, buy-then-sell realises 750 where sell-then-buy
-- realises 1000. domain.Holding.Position sorts STABLY, which means it keeps
-- whatever order it was handed for a tie -- so the tie has to be broken here,
-- by the order the events were actually recorded in.
--
-- Do not drop created_at or id from this index thinking they are noise. They
-- are what makes the answer deterministic.
CREATE INDEX holding_events_fold_idx
    ON holding_events (holding_id, occurred_on, created_at, id);

-- holding_valuations is what one unit was worth on a given day.
--
-- unit_price_minor is per unit, unlike holding_events.amount_minor which is
-- the whole event. A price is genuinely per-unit: it is what the owner reads
-- off a screen.
--
-- as_of is the day the price was TRUE, not the day it was typed. The two
-- differ whenever someone backfills a quarter, and the period report needs the
-- former.
CREATE TABLE holding_valuations (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    holding_id        uuid        NOT NULL REFERENCES holdings(id),
    household_id      uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    unit_price_minor  bigint      NOT NULL CHECK (unit_price_minor >= 0),
    primary_unit_price_minor bigint,
    primary_currency         char(3),
    as_of             date        NOT NULL,
    note              text        NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT valuation_primary_price_is_whole CHECK (
        (primary_unit_price_minor IS NULL) = (primary_currency IS NULL)
    ),
    CONSTRAINT valuation_primary_price_not_negative CHECK (
        primary_unit_price_minor IS NULL OR primary_unit_price_minor >= 0
    ),
    -- One price per holding per day. Re-entering a day's price is a
    -- correction, not a second opinion, so it upserts -- which also removes
    -- the question of which of two rows for one day the report should believe
    -- before anyone can ask it.
    UNIQUE (holding_id, as_of)
);

-- The portfolio page wants the newest valuation per holding, so the index runs
-- the way that read does.
CREATE INDEX holding_valuations_latest_idx
    ON holding_valuations (holding_id, as_of DESC);

-- +goose Down
DROP TABLE holding_valuations;
DROP TABLE holding_events;
DROP TABLE holdings;
