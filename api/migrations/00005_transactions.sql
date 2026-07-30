-- +goose Up

-- categories is the household's own spending taxonomy. It ships with
-- Transactions rather than with Budget because the design's own
-- "Log a transaction" modal has a Category dropdown, and Budget's envelopes
-- are sums over these rows.
--
-- A household's set is created the first time anything reads it (see
-- CategoryService), not at household creation: seeding here would reach into
-- SignupRepository.Provision, the transaction a stranger's sign-up depends
-- on, for a feature that does not need it. See the spec's decision 1.
CREATE TABLE categories (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('expense', 'income')),
    -- The order the design draws, not alphabetical: sorting by name would put
    -- "Dining out" above "Groceries" for no reason a household would recognise.
    sort_order   integer     NOT NULL,
    -- Budget's "Edit categories" screen archives rather than deletes, so a
    -- category that transactions already reference keeps its name. It also
    -- keeps its unique key, which is what stops a household that cleared its
    -- list from being silently re-seeded.
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Load-bearing, not a nicety: this is the conflict target that makes
    -- EnsureSeeded idempotent under two simultaneous first requests.
    UNIQUE (household_id, name)
);

-- transactions is what happened to a household's accounts.
--
-- One row per event, including a transfer, which carries both of its accounts.
-- Two mirrored legs would keep the balance sum uniform and were rejected: two
-- rows that must always be written, edited and deleted together is precisely
-- the partial-write shape four defects in this project have had. One row
-- cannot half-exist. See the spec's decision 2.
CREATE TABLE transactions (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id             uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    kind                     text        NOT NULL
                                         CHECK (kind IN ('expense', 'income', 'transfer')),
    -- A date, not a timestamptz: "18 July" is a fact about a day. This product
    -- stores no timezone for a household, so an instant would mean different
    -- days to the server and to the person who typed it.
    occurred_on              date        NOT NULL,
    description              text        NOT NULL,
    -- ON DELETE SET NULL on both references, for the reason accounts uses it
    -- for a removed member's accounts: losing a label is the least valuable
    -- thing on the row, and refusing the deletion would mean an owner cannot
    -- remove a departed member without first reassigning every transaction
    -- they ever paid for.
    category_id              uuid        REFERENCES categories(id) ON DELETE SET NULL,
    paid_by_membership_id    uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    -- ON DELETE CASCADE, and RESTRICT would be wrong. The application never
    -- deletes an account -- accounts archive -- so this clause is unreachable
    -- in ordinary use. It fires in exactly one case: deleting a household
    -- cascades to its accounts, and a RESTRICT here would make that cascade
    -- fail with a foreign key violation.
    from_account_id          uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    to_account_id            uuid        REFERENCES accounts(id) ON DELETE CASCADE,
    -- Stored positive; whether it adds or subtracts comes from kind and from
    -- which side of the row the account sits on. Letting someone type a
    -- negative expense makes "I typed -52.30 for groceries and my balance went
    -- up" representable. See the spec's decision 9.
    amount_minor             bigint      NOT NULL CHECK (amount_minor > 0),
    amount_currency          char(3)     NOT NULL,
    -- What actually arrived in the destination account, in that account's own
    -- currency: required when a transfer crosses currencies, optional when it
    -- does not, so a bank fee on a same-currency transfer is recordable. NULL
    -- means "the same figure that left". See the spec's decision 3.
    received_amount_minor    bigint,
    received_amount_currency char(3),
    created_at               timestamptz NOT NULL DEFAULT now(),

    -- The constraint that makes a nonsense row impossible: an expense with a
    -- destination, a transfer with one leg, a transfer from an account to
    -- itself. Every one of those produces a balance that is wrong with nothing
    -- on screen to explain it.
    CONSTRAINT accounts_match_kind CHECK (
        (kind = 'expense'  AND from_account_id IS NOT NULL AND to_account_id IS NULL)
     OR (kind = 'income'   AND to_account_id IS NOT NULL AND from_account_id IS NULL)
     OR (kind = 'transfer' AND from_account_id IS NOT NULL AND to_account_id IS NOT NULL
                           AND from_account_id <> to_account_id)
    ),
    CONSTRAINT received_amount_pairs CHECK (
        (received_amount_minor IS NULL) = (received_amount_currency IS NULL)
    ),
    CONSTRAINT received_amount_is_a_transfer_thing CHECK (
        kind = 'transfer' OR received_amount_minor IS NULL
    ),
    CONSTRAINT received_amount_is_positive CHECK (
        received_amount_minor IS NULL OR received_amount_minor > 0
    ),
    -- The Transactions screen's own banner says a category feeds Budget spend.
    -- A transfer is not spend, so it cannot carry one.
    CONSTRAINT transfer_has_no_category CHECK (
        kind <> 'transfer' OR category_id IS NULL
    )
);

-- Column order is the sort order the ledger reads in, so the keyset cursor in
-- ListTransactions can walk this index rather than sorting a heap.
CREATE INDEX transactions_household_date_idx
    ON transactions (household_id, occurred_on DESC, id DESC);

-- The accounts-balance sum filters by each side; without these it degrades to
-- a sequential scan of every transaction in the database per account listed.
CREATE INDEX transactions_from_account_idx ON transactions (from_account_id);
CREATE INDEX transactions_to_account_idx   ON transactions (to_account_id);

-- +goose Down
DROP TABLE transactions;
DROP TABLE categories;
