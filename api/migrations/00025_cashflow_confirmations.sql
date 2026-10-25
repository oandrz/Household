-- +goose Up
-- A household owner can mark a finished month the cashflow page judged "not
-- enough data" as fine. One row per household and month, shared by every
-- member so both partners see the same answer.
--
-- Limit (ADR 13): the service only accepts months from 2000-01 up to the last
-- finished month, so a household holds at most one row per month in that
-- window. That is a small fixed ceiling, so no LimitKind is needed.
--
-- The member who confirmed may leave: the row stays and the name reads "".
CREATE TABLE cashflow_month_confirmations (
    household_id               uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    month                      date        NOT NULL
        CHECK (month = date_trunc('month', month)::date AND month >= DATE '2000-01-01'),
    confirmed_by_membership_id uuid        REFERENCES memberships(id) ON DELETE SET NULL,
    confirmed_at               timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, month)
);

-- +goose Down
DROP TABLE cashflow_month_confirmations;
