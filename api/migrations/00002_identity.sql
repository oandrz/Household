-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

DROP TABLE IF EXISTS schema_smoke;

CREATE TABLE households (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    text        NOT NULL,
    family_name             text        NOT NULL,
    primary_currency        char(3)     NOT NULL DEFAULT 'SGD',
    show_secondary_currency boolean     NOT NULL DEFAULT true,
    secondary_currency      char(3)     NOT NULL DEFAULT 'IDR',
    fx_rate_mode            text        NOT NULL DEFAULT 'auto'
                                        CHECK (fx_rate_mode IN ('auto', 'manual')),
    created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email          citext UNIQUE,
    password_hash  text,
    display_name   text        NOT NULL,
    avatar_initial char(1)     NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         text        NOT NULL CHECK (role IN ('owner', 'limited')),
    capabilities text[]      NOT NULL DEFAULT '{}',
    joined_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, user_id),
    CONSTRAINT capabilities_are_known CHECK (
        capabilities <@ ARRAY['calendar', 'chores', 'money', 'marriage']::text[]
    ),
    CONSTRAINT limited_members_have_no_marriage CHECK (
        role <> 'limited' OR NOT ('marriage' = ANY (capabilities))
    ),
    -- Mirrors the domain rule that an owner holds every capability. The domain
    -- constructor is the first gate; this is the second, for rows written by
    -- anything that bypasses it.
    CONSTRAINT owners_hold_all_capabilities CHECK (
        role <> 'owner'
        OR capabilities @> ARRAY['calendar', 'chores', 'money', 'marriage']::text[]
    )
);

CREATE TABLE invites (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    name         text        NOT NULL,
    role         text        NOT NULL CHECK (role IN ('owner', 'limited')),
    capabilities text[]      NOT NULL DEFAULT '{}',
    token_hash   bytea       NOT NULL UNIQUE,
    invited_by   uuid        NOT NULL REFERENCES users(id),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   bytea       NOT NULL UNIQUE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE magic_links (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX magic_links_user_created_idx ON magic_links (user_id, created_at DESC);

-- household_id and user_id are nullable so that an attempt against an unknown
-- address can still be recorded for global rate limiting without revealing
-- whether that address exists.
CREATE TABLE login_attempts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid REFERENCES households(id) ON DELETE CASCADE,
    user_id      uuid REFERENCES users(id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    succeeded    boolean     NOT NULL,
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_household_at_idx ON login_attempts (household_id, at DESC);

CREATE TABLE spaces (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id        uuid    NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key                 text    NOT NULL,
    name                text    NOT NULL,
    visibility          text    NOT NULL CHECK (visibility IN ('everyone', 'parents_only', 'custom')),
    position            integer NOT NULL,
    is_builtin          boolean NOT NULL DEFAULT false,
    required_capability text    NOT NULL DEFAULT '',
    UNIQUE (household_id, key)
);

CREATE TABLE notification_preferences (
    household_id     uuid PRIMARY KEY REFERENCES households(id) ON DELETE CASCADE,
    bill_reminders   boolean NOT NULL DEFAULT true,
    overspend_alerts boolean NOT NULL DEFAULT true,
    retro_reminder   boolean NOT NULL DEFAULT true,
    weekly_digest    boolean NOT NULL DEFAULT true
);

-- +goose Down
DROP TABLE notification_preferences, spaces, login_attempts, magic_links,
           sessions, invites, memberships, users, households;
CREATE TABLE schema_smoke (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now()
);
