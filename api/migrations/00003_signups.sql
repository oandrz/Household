-- +goose Up

-- signups holds a verified-address intent, and nothing else: no household
-- name, no display name, no password. Those are collected on the screen the
-- mailed token leads to, deliberately -- if they were captured before
-- verification, someone could submit a sign-up for another person's address
-- with a household name and display name of their choosing, and the mail
-- would invite that person into a household a stranger had configured.
--
-- Shaped after magic_links, with one difference: there is no user_id, because
-- there is no user yet. That absence is the reason this is a new table rather
-- than a column on magic_links -- magic_links.user_id is NOT NULL REFERENCES
-- users(id), and relaxing it would put a nullable branch on the recovery path
-- that has to keep working while a household is locked.
CREATE TABLE signups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       citext      NOT NULL,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- email is deliberately NOT unique. Several live tokens for one address are
-- fine: the first consumed wins, and a second consume then collides with
-- users.email's unique index, which translate() turns into
-- domain.ErrAlreadyExists and MapDomainError answers 409. Making email unique
-- here would instead make a second sign-up request for the same address fail
-- loudly, which is itself an enumeration oracle.
CREATE INDEX signups_email_created_idx ON signups (email, created_at DESC);

-- Supports both the global daily mail ceiling and PruneSignups.
CREATE INDEX signups_created_idx ON signups (created_at DESC);

-- avatar_initial was char(1). One character is enough for a single letter in
-- any script, so this is not about non-ASCII names fitting -- it is about
-- strings.ToUpper growing a rune: German 'ß' uppercases to "SS", two
-- characters, which char(1) rejects outright. Nobody could reach that before,
-- because initialOf sliced bytes and produced mojibake long before it reached
-- the column; fixing initialOf to slice runes is what makes the expansion
-- case reachable. text also leaves room for a future profile editor to store a
-- two-character initial.
ALTER TABLE users ALTER COLUMN avatar_initial TYPE text;

-- +goose Down
ALTER TABLE users ALTER COLUMN avatar_initial TYPE char(1);
DROP TABLE signups;
