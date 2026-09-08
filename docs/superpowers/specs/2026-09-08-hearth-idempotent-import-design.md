# Idempotent transaction writes, and `hearthctl transaction import` — design

**Date:** 2026-09-08. **Status:** built in the same change as this spec, on
branch `hearthctl-agent`, stacked on `hearthctl`.

## Why

`hearthctl` (ADR 6) made every write reachable from a script or an agent, and
recorded one gap honestly: the API has no idempotency keys, so a write that
times out (exit 4) may or may not have landed, and a retry is a duplicate row.
An agent importing a bank statement is exactly the caller that will retry. Two
things close the gap: the server learns to recognise a repeated create, and
the CLI learns to send many rows with a key on each.

## Decisions

1. **The key lives on the row.** `transactions.idempotency_key text`,
   nullable, with a partial unique index on `(household_id, idempotency_key)
   WHERE idempotency_key IS NOT NULL`. No separate keys table: the key's
   lifetime is the row's, so there is no retention job, and two concurrent
   inserts with one key are settled by the index rather than by application
   code. Migration `00015`.
2. **The key travels in a header**, `Idempotency-Key`, on `POST /transactions`
   only. The body DTO is unchanged, so the frontend is untouched. No header
   means today's behaviour exactly.
3. **Replay is fail-closed.** On a unique violation the service loads the
   stored row by key and compares the request to it field by field: kind,
   date, description (trimmed), the four ids, amount and received amount. A
   match answers the stored row with **200** (a fresh create stays 201).
   A mismatch is `409 IDEMPOTENCY_KEY_REUSED`. Silently handing back a
   different transaction than the one asked for is LEARNING pattern 5.
4. **One honest edge.** The id columns are `ON DELETE SET NULL`, so a replay
   after the category or payer was removed compares unequal and gets 409.
   Accepted: the caller learns the row changed under it rather than being
   told the retry "worked".
5. **Delete frees the key.** A deleted row's key is gone with it, so
   re-running an import recreates that row. No tombstones.
6. **Key validation is domain.** 1–128 characters, printable ASCII, no
   spaces. Anything else is `422 IDEMPOTENCY_KEY_INVALID`. Fail closed on a
   value the server did not construct.
7. **Only transactions get this now.** Bills, goals and accounts are created
   once by hand; a transaction is the row an agent creates hundreds of. When
   a second table needs it, the pattern is copied, not generalised early.

## The CLI side — `hearthctl transaction import <file.csv> [--dry-run]`

8. **No new server route.** The import is a client-side loop of
   `POST /transactions`, one key per row. Rows are independent and replay-safe,
   so a loop is correct and the server stays simple.
9. **Header row is required**, columns in any order:
   `date, kind, description, amount_minor, from_account, to_account, category,
   paid_by, key, received_minor`. Unknown columns are an error.
10. **Names or ids.** `from_account`, `to_account`, `category`, `paid_by`
    accept a UUID or a name. The three lists are fetched once; a name must
    match exactly one entry, case-insensitively; unknown or ambiguous is a
    per-row error.
11. **Resolve everything before the first POST.** Every row is parsed and
    resolved first; `--dry-run` stops there and prints the resolved rows.
12. **Default key.** When the `key` column is empty, the key is
    `sha256(date|kind|description|amount|from|to|category|paid_by|received)`
    truncated to 32 hex characters, plus `#n` for the n-th identical row in
    the same file (n ≥ 2). A file replayed is a no-op; two genuine identical
    coffees stay two rows; an edit above a row does not change that row's
    key. Line numbers alone would duplicate everything below an edit.
13. **Continue past a failed row.** The summary on stdout is
    `{"created":n,"replayed":n,"failed":[{"line":n,"error":…}]}`; exit 3 if
    anything failed, else 0.
14. `transaction add --key=<k>` and `api --idempotency-key=<k>` get the same
    header for single writes.

## Files

```
api/migrations/00015_transaction_idempotency_key.sql
api/internal/adapter/postgres/queries/transaction.sql   CreateTransaction gains the column; GetTransactionByIdempotencyKey
api/internal/domain/transaction.go                      IdempotencyKey field, ValidateIdempotencyKey
api/internal/domain/errors.go                           ErrIdempotencyKeyInvalid, ErrIdempotencyKeyInUse, ErrIdempotencyKeyReused
api/internal/usecase/ports.go                           GetByIdempotencyKey on TransactionRepository
api/internal/usecase/transaction.go                     CreateOrReplay
api/internal/adapter/postgres/transaction_repo.go       the two methods, the constraint name
api/internal/adapter/http/transaction_handlers.go       the header, 200 vs 201
api/internal/adapter/http/errors.go                     the two new codes
api/cmd/hearthctl/cmd_import.go                         the import
```

## Testing

- Usecase: same key twice returns the first row and `replayed=true`; same
  key different body returns `ErrIdempotencyKeyReused`; the fake repository
  enforces the unique rule so the replay branch is actually taken.
- HTTP: same key twice → one row, 201 then 200; same key different body →
  409; no header → two rows; bad key → 422; the header present but empty →
  422 (not silently "no key"). "Same key in two households → two rows" is
  pinned at the usecase level against the fake repository and by the
  index's own column list (`household_id, idempotency_key`, seen live with
  `\d transactions`); the HTTP suite has no second-household fixture, so
  it is not asserted there.
- Postgres: the partial index exists and fires (testcontainers).
- CLI: key derivation (identical rows get `#2`), name resolution
  (ambiguous refused), summary counts, exit 3 on a failed row.
- Real environment: `make migrate`, import a CSV twice, ledger in a browser
  shows each row once; 409 path live.
