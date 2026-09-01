-- +goose Up

-- Platform admin is a different axis from household role. A member's Role
-- (owner/limited) and Capabilities answer "what may this person do inside
-- their own household"; this table answers "who runs this install". The two
-- must never be expressed in terms of each other, which is why this is a
-- table of its own rather than a column on memberships.
--
-- Rows are created only by adminctl, never over HTTP: an admin surface that
-- can mint its own admins turns one stolen session into permanent access.
CREATE TABLE platform_admins (
    user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    note       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The re-auth grant. The session cookie lives 30 days, which is right for a
-- household member and far too long for a surface that can read every
-- household's data, so entering /admin costs the password again and stamps a
-- short expiry here.
--
-- On the session row rather than in a second cookie: sign-out already revokes
-- the session, so the grant dies with it for free, and there is no second
-- cookie whose Secure/SameSite/HttpOnly flags could be got wrong independently
-- of the first.
ALTER TABLE sessions ADD COLUMN admin_grant_expires_at timestamptz;

-- Feature flag overrides. The registry of flags is compile-time
-- (domain.AllFlags), so there is deliberately no table of flag definitions and
-- no foreign key on `key`: a row here can outlive the const that named it.
-- Resolution ignores keys domain.ParseFlag refuses, so an orphaned row can
-- never turn anything on -- see domain.ResolveFlags.
--
-- A row exists only where somebody overrode something. An install with no rows
-- at all behaves exactly as AllFlags says.
CREATE TABLE feature_flags (
    key        text PRIMARY KEY,
    enabled    boolean     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid        REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE household_feature_flags (
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    key          text        NOT NULL,
    enabled      boolean     NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   uuid        REFERENCES users(id) ON DELETE SET NULL,
    PRIMARY KEY (household_id, key)
);

-- Append-only. No delete route exists anywhere in the product and adminctl
-- prune does not touch this table: a log the operator can edit stops meaning
-- anything the moment the operator makes a mistake.
--
-- target, detail and ip default rather than being NOT NULL without one,
-- because auditAdmin writes from middleware where there is not always a target
-- to name. detail records what was looked at, never what was seen -- no
-- passwords, no tokens, no row values.
--
-- actor_user_id deliberately does not cascade: deleting a user with audit
-- history must fail loudly rather than quietly taking the record of what they
-- did with them.
CREATE TABLE admin_audit_log (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid        NOT NULL REFERENCES users(id),
    action        text        NOT NULL,
    target        text        NOT NULL DEFAULT '',
    detail        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip            text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_audit_log_created_at_idx ON admin_audit_log (created_at DESC);

-- Admin re-auth failures get their own ledger rather than login_attempts,
-- because that table's lockout is HOUSEHOLD-scoped: three failures lock
-- password sign-in for every member. Feeding an operator's mistypes into it
-- would lock their whole household out of the ordinary product as a side
-- effect of fumbling a password on a screen nobody else can see.
--
-- The policy is shared even though the ledger is not: domain.LockoutPolicy
-- evaluates both.
CREATE TABLE admin_reauth_attempts (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    succeeded boolean     NOT NULL,
    at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_reauth_attempts_user_at_idx ON admin_reauth_attempts (user_id, at DESC);

-- +goose Down
DROP TABLE admin_reauth_attempts;
DROP TABLE admin_audit_log;
DROP TABLE household_feature_flags;
DROP TABLE feature_flags;
ALTER TABLE sessions DROP COLUMN admin_grant_expires_at;
DROP TABLE platform_admins;
