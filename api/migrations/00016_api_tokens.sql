-- +goose Up

-- A member's own long-lived credential for scripts and agents: the thing
-- ADR 6 deferred and docs/superpowers/specs/2026-09-08-hearth-api-tokens-
-- design.md builds. One row per token; the raw value is shown once at
-- creation and only its SHA-256 is kept here, the same rule sessions follow.
--
-- A token belongs to a user *and* names a household, like a session, so the
-- request it authenticates gets exactly the Scope a session would. Both
-- references CASCADE: a token cannot outlive the person or the household it
-- speaks for.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- What the person called it ("laptop cron", "claude"), so a list of
    -- tokens is a list of purposes rather than a list of dates.
    name         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    -- The first characters of the raw token, so a list can say which token
    -- is which without holding the secret. Not unique and not a credential.
    prefix       text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Required, unlike sessions' extend-on-use: a token that lives forever
    -- is a credential nobody rotates. The service bounds it at 365 days.
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

-- `token list` and "revoke everything this member holds" both read by user.
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE api_tokens;
