-- +goose Up

-- A caller-supplied key that makes POST /transactions safe to retry. An
-- agent importing a statement over a flaky connection cannot otherwise tell
-- "timed out before the insert" from "timed out after it", and a blind retry
-- is a duplicate row (docs/adr/0006, docs/superpowers/specs/
-- 2026-09-08-hearth-idempotent-import-design.md).
--
-- NULL means "no key was given", which is every row the web app writes. The
-- key lives on the row rather than in its own table so its lifetime is the
-- row's: no retention job, and a deleted transaction frees its key
-- (decision 5). The partial unique index is the whole race guard -- two
-- concurrent inserts with one key are settled here, not in Go.
ALTER TABLE transactions ADD COLUMN idempotency_key text;

CREATE UNIQUE INDEX transactions_household_idempotency_key
    ON transactions (household_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX transactions_household_idempotency_key;
ALTER TABLE transactions DROP COLUMN idempotency_key;
