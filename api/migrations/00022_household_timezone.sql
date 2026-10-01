-- +goose Up

-- A household keeps one calendar (ADR 12): this IANA zone name decides which
-- day "today" is for everyone in it. The server computes that day from it on
-- every request, and the frontend from the same value, so the two cannot
-- disagree about the date or the month.
--
-- DEFAULT 'UTC' is for a row written without a zone; sign-up sends the
-- browser's. The empty string is refused here because Go loads "" as UTC
-- without complaint, which would look like a stored choice and not be one.
-- A name that is not a real zone cannot be checked in SQL against the zone
-- data the API carries, so that check is domain.ParseTimezone's.
ALTER TABLE households ADD COLUMN timezone text NOT NULL DEFAULT 'UTC'
    CONSTRAINT households_timezone_not_empty CHECK (timezone <> '');

-- Every household that exists when this runs lives in Singapore. Without
-- this they would stay on the UTC calendar that gave them the wrong day for
-- eight hours of every day.
UPDATE households SET timezone = 'Asia/Singapore';

-- +goose Down
ALTER TABLE households DROP COLUMN timezone;
