-- +goose Up

-- When this session was last used, refreshed by requireSession at most once
-- an hour (middleware_session.go's sessionTouchInterval). It exists so the
-- operator's "active in the last 7 days" counter means *used*, not *signed
-- in*: a session lives 30 days and is extended in place, so created_at
-- alone reads a daily user as gone for a month.
--
-- NULL means "not touched since this column existed". Every reader treats
-- that as created_at -- COALESCE(last_seen_at, created_at) is the one
-- expression for "when was this session last used", and it lives in
-- queries/admin_directory.sql alone so it cannot drift. No backfill, no
-- index: the spec's §3 says when an index would be earned.
ALTER TABLE sessions ADD COLUMN last_seen_at timestamptz;

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_seen_at;
