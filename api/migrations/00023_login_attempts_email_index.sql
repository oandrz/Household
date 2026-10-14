-- +goose Up
-- Sign-in counts an address's recent failures by email when the address has
-- no household, and anyone can make such attempts. The table's only other
-- index leads with household_id, so without this one that count reads every
-- row in the table.
CREATE INDEX login_attempts_email_at_idx ON login_attempts (email, at DESC);

-- +goose Down
DROP INDEX login_attempts_email_at_idx;
