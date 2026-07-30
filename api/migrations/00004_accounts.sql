-- +goose Up

-- accounts is what a household owns and owes. One row per account; the
-- balance is an opening figure plus, from the next slice, every transaction
-- dated on or after opening_balance_as_of.
--
-- There is deliberately no updated_at. No other table in this schema has one,
-- nothing in the application would maintain it, and a column named "last
-- updated" that never changes is a lie the next reader will believe. The
-- question it would answer for an account -- when was this balance last true
-- -- is answered better by opening_balance_as_of.
CREATE TABLE accounts (
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id               uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    nickname                   text        NOT NULL,
    type                       text        NOT NULL
                                           CHECK (type IN ('cash', 'investment', 'property',
                                                           'loan', 'credit_card')),
    -- NULL means shared by the whole household. ON DELETE SET NULL is what
    -- makes a removed member's accounts fall back to shared with no
    -- application code running: refusing the removal instead would mean an
    -- owner cannot clean up a departed member without first reassigning every
    -- account they own, and deleting the accounts would destroy financial
    -- history that transactions will soon depend on.
    --
    -- It references memberships rather than users because an account belongs
    -- to someone *in this household*; a user reference would let an account
    -- name somebody who is not a member.
    owner_membership_id        uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    opening_balance_minor      bigint      NOT NULL,
    -- The currency is stored per account, not inherited from the household. A
    -- household's primary currency can change in Settings; this balance was
    -- denominated in whatever it was denominated in, and restating it would
    -- silently rewrite history.
    opening_balance_currency   char(3)     NOT NULL,
    opening_balance_as_of      date        NOT NULL,
    count_toward_net_worth     boolean     NOT NULL DEFAULT true,
    visible_to_limited_members boolean     NOT NULL DEFAULT false,
    archived_at                timestamptz,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    -- The second line of defence for domain.AccountType.SignedNetWorthAmount:
    -- a debt's amount is the sum owed, as a positive number, and the minus
    -- sign is derived from the type. Worth enforcing twice because the failure
    -- it prevents -- a debt counted as an asset -- is silent and wrong in the
    -- flattering direction.
    CONSTRAINT liabilities_are_not_negative CHECK (
        type NOT IN ('loan', 'credit_card') OR opening_balance_minor >= 0
    )
);

-- Matches the query the accounts list actually runs: live accounts for one
-- household. Archived accounts are read rarely and can use a sequential scan.
CREATE INDEX accounts_household_idx ON accounts (household_id) WHERE archived_at IS NULL;

-- +goose Down
DROP TABLE accounts;
