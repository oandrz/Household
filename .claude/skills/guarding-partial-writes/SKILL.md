---
name: guarding-partial-writes
description: Make sure an operation that writes more than once either completes fully or fails loudly, and that a function persists everything it accepts. Use when writing or reviewing any create/update flow, any handler with a request struct, any repository method, and any code where two things must both happen. Four defects here returned success for work that had only partly happened — one of them made an invite permanently unacceptable with no way to recover.
---

# Guarding partial writes

The failure this prevents is not a crash. It is a `nil` error returned for work
that did not fully happen — indistinguishable from success, and therefore never
investigated until someone notices the data is wrong.

Four real instances from this project:

- **`HouseholdRepository.Update` accepted six fields and persisted four.** A caller
  setting the other two got `nil` back. The signature promised more than the SQL
  delivered.
- **`PATCH /household` blanked every field the caller omitted.** The spec's own
  documented request body returned a 500. The handler's doc comment claimed it
  read the current record first precisely so omitted fields would not be blanked —
  and then blanked them.
- **Invite acceptance was three separate writes.** A failure in the middle left an
  orphaned user row holding the unique email index, so the invite could **never**
  be accepted — by anyone, ever, with no path forward short of manual SQL.
- **Creating a child was two writes with no transaction.** Because a child's email
  is NULL there is no unique constraint to fail loudly, so each retry silently
  created *another* orphan. Three retries, three zombie rows, discoverable only by
  reading the table.

## Two questions

### 1. Does this function persist everything it accepts?

If a signature takes a field, it must write it or refuse it. Silently ignoring it
is the worst option, because the caller has no way to tell.

- Compare the struct against the `UPDATE` column list, field by field
- For a `PATCH`, use pointers so *absent* and *empty* are distinguishable, and
  apply only what is present. A `PATCH` that overwrites omitted fields is a `PUT`
  wearing the wrong name
- Test it by setting **every** field, reading it back, and asserting each one
  changed — then by setting exactly one and asserting the others did not

That last test is the one that stops a future narrowing of the query from
silently dropping writes.

### 2. If the second write fails, what is left behind?

Trace it concretely. Not "it would error" — *what rows exist afterwards, and can
the user recover?*

Three outcomes, in descending order of how much you should worry:

- **Unrecoverable.** Something survives that blocks the retry — a unique index
  held by a half-created row. This is the worst case and it looks identical to
  the merely-annoying one until you try again.
- **Silently accumulating.** Nothing blocks the retry, so each attempt leaves more
  debris. Nullable columns and missing constraints turn a loud failure into a
  quiet one.
- **Clean.** The failed write left nothing. Fine.

If the answer is either of the first two, the writes belong in one transaction.
In this codebase that means beginning a `pgx.Tx`, binding `sqlcgen.Queries` with
`WithTx`, running every statement on that binding, committing, and deferring a
rollback that is a no-op after commit. `InviteRepo.Accept` is the worked example.

**Test it by forcing the second statement to fail** — a check constraint is the
easiest lever — and asserting nothing from the first survives. That test is the
entire point; without it the transaction is an assumption.

## The related trap: a partial success reported as failure

Not every multi-step operation can be atomic. When a follow-up action fails after
the main write succeeded, the caller must be able to tell the difference between
"nothing happened" and "it happened, but the follow-up did not".

Here, a member's role change committed and the session revocation that follows it
failed — and the caller got a bare error indistinguishable from the change never
happening. Meanwhile the change *had* happened and the member's old session was
still live, which is the exact thing revocation exists to prevent.

Give that case its own error, log it with enough context to act on, and say in a
comment why the main write is not rolled back.

## Quick checklist

- Every field in the request struct appears in the write
- `PATCH` uses pointers; absent means unchanged
- Two dependent writes are in one transaction, or the failure is provably clean
- There is a test that forces the *second* write to fail
- A partial success is distinguishable from a total failure
- Nullable columns are not quietly hiding a missing constraint
