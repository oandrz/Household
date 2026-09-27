# Readability item 0 — the backend comment sweep — design

Date: 2026-09-27 · Status: approved in brainstorming, awaiting spec review ·
Route: Plan (`superpowers:writing-plans`, then `superpowers:executing-plans`,
then `/code-review`)

## Why

The owner wants the backend easier for them to maintain. Yesterday's
architecture review (`docs/reviews/2026-09-26-architecture-review.md`, kept
untracked) already ranks the structural work, items 1–14. This spec adds an
**item 0** in front of what is left of that list, because the thing that most
slows down reading the backend today is not structure but comments:

- Comment lines are 39% of `usecase` production code and 34% of `domain`.
- 186 production comments and 192 test comments cite history ("decision 22",
  "Task 6", "milestone 2") instead of stating the rule. The numbers point into
  spec and plan files a reader has to open to understand one line.
- Many comments tell the story of earlier shapes ("the earlier shape…",
  "once Bills ships…") that no longer matter to someone changing the code.

Item 14 was going to fix this "alongside each item". At Design-route pace that
takes months. Item 0 does it once.

### What the scan found that is *not* in scope

The owner also asked to remove dead code. There is none to remove:
`make lint-dead` already fails the build on unreachable code. A stricter run
without `-test` flagged two functions, and both are false positives:

- `openrouter.WithBaseURL` is a test seam; tests point the parser at a fake
  server with it.
- `domain.TokenState.String` is called by `fmt` through the `Stringer`
  interface, which the tool cannot see.

Structural and SOLID improvements are items 2a–13 of the ranked list and stay
there, in that order.

## The guarantee

**Item 0 changes no code: only comments** (plus six test failure messages,
see below). Every commit that touches Go or SQL is proven by a tool to leave
the code tokens identical. That is what makes a diff this large safe to
review: the reviewer reads it for wording, not for bugs.

## Scope

In scope:

- Every `.go` file under `api/`: production code, tests and the hand-written
  test doubles (`usecase/testdouble_test.go` and the other doubles), all
  trimmed at the same level.
- The comments in `api/internal/adapter/postgres/queries/*.sql` (17 files,
  1,096 comment lines), followed by `make sqlc` so the generated
  `sqlcgen/*.sql.go` files carry the new text.

Out of scope, and why:

| Left out | Reason |
|---|---|
| `api/migrations/` | A migration is the record of what already ran against a database. Editing it gains nothing and risks a checksum or a reader's trust. |
| Moving `usecase/seed.go` out of `usecase` | Its 582-line test is built on the `usecase` package's in-memory doubles; moving it means rewriting that test against a real database. That is a code change. It stays on item 14. |
| Renaming `MailOutbox` | "Outbox" names the whole feature: the admin route, the screen, `AdminOutboxService`, `OutboxPage`, `ErrOutboxUnavailable` and 8 documents. Renaming the port alone makes naming less consistent; renaming it all is a feature-wide change. It stays on item 14. |
| Frontend (`web/`) | The request was the backend. |
| Any behaviour, name, signature, file move or file split | Items 1–14 own those. |

Comments that a tool reads are never edited: `//go:build`, `// +build`,
`//go:embed`, `//go:generate`, `//go:linkname`, `//export`, `//line`,
`//nolint`, `// Output:` and `// Unordered output:` in examples, and sqlc's
`-- name:` lines.

## The comment rules (the "Moderate" level)

The owner chose Moderate from three levels shown on the same real comment:
keep every *why*, say it once and short. The rules:

| Comment kind | Action |
|---|---|
| History reference: `decision N`, `spec decision N`, `Task N`, `milestone N` | Replace the reference with the rule it stands for. When the reasoning is too long to restate, name the spec file once (`see docs/superpowers/specs/2026-09-05-hearth-agreements-design.md`), never a decision number. |
| History narration: earlier shapes, "once X ships", "was X, now Y" | Delete. Keep only when someone could plausibly reintroduce the mistake, and then write it as a warning: "Don't X: Y." |
| Restates the code | Delete. |
| `file.go:123` line reference | Replace with the symbol (`domain.Bill.DueIn`). |
| Doc comment on an exported identifier | Always keep one, starting with the identifier's name. Trim it to the contract: what it does, its errors, its invariants. |
| A *why*, a trade-off, an invariant, a security or ordering reason | Keep, once, in as few lines as it takes. This is what Moderate protects. |
| `ADR N`, `LEARNING pattern N` | Keep. They point at durable documents, not at plan steps. |
| Test comments | The test name states the behaviour. A body comment stays only when the setup is not obvious (why *this* data). |
| Test doubles | Trim fully, but keep every note that says where a double behaves differently from Postgres. Item 12 needs exactly those notes. |

Worked example, `usecase.BudgetPersonView`, 11 lines to 5:

```go
// BudgetPersonView is one row of "Spending by person".
//
// MembershipID "" collects spend with no payer on file. Keep that row:
// without it the rows sum to less than Spent and nothing on screen explains
// the gap. Its Name is empty on purpose - the frontend owns all copy here.
```

## Stopping the comments growing back

Specs number their decisions, and the agents that wrote the code cited the
numbers. Without a guard the next feature adds new ones. Two guards:

1. **A lint check.** A new `lint-comments` target, run by `make lint`, fails on
   `decision [0-9]`, `Task [0-9]`, `[Mm]ilestone [0-9]` or a
   `\.(go|sql|ts|tsx):[0-9]` reference that follows a comment marker: `//` in
   any `.go` file under `api/`, and `--` in `queries/*.sql` and in `sqlcgen/`
   (which copies the SQL comments into strings). It looks only at comments,
   because a product word in a string or an identifier is not history. It is
   a grep in the Makefile, not a program. It must be shown to fail on a
   planted violation before it counts.
2. **A "Comments" block in `CLAUDE.md`** under "Readable by a junior engineer",
   condensing the rules table above to a few bullets.

## Proving no code changed

A throwaway Go program (about 40 lines, standard library only) compares the
tree at the branch's base commit with the working tree:

1. The set of `.go` files is identical: none added, removed or renamed.
2. For each `.go` file, the token stream from `go/scanner` with comments
   skipped (each token's kind and literal, positions ignored) is identical.
3. For each `.go` file, the list of tool-read comments listed under Scope is
   identical, in order.
4. For each `queries/*.sql` file, the lines that are not comments are
   identical, and every `-- name:` line is identical.
5. For each production `.go` file outside `sqlcgen/`, every exported
   function, method, type, const and var that had a doc comment still has
   one.

Two narrow allowances, each still checked mechanically:

- **Generated sqlc files.** 158 SQL comment lines sit inside query bodies, and
  sqlc copies them into the query string constants in `sqlcgen/*.sql.go`. So
  for files under `sqlcgen/`, each string literal is compared with its
  `--` comment lines removed. Everything else in those files must match
  exactly. `make sqlc` is the only thing that writes them, and running it a
  second time must produce no diff.
- **Test failure messages.** Six `t.Fatalf`/`t.Errorf` strings in test files
  cite a decision or task number (for example `"(spec decision 14)"`): four in
  `usecase` tests, two in `adapter/http` tests. In the commits that own those
  files, and only in `_test.go` files, string literals may differ, and the
  tool prints every one that did. That list must contain only those six
  messages, now stating the rule instead of the number. Production strings may
  never differ.

Before it is trusted it is proven able to fail (`proving-tests-can-fail`):
change one identifier, one string literal and one `//go:` directive in turn,
and see each one reported. It is not committed; its source goes into the PR
description so anyone can re-run it.

The tool cannot see a *why* deleted by mistake. So after each package is
trimmed, a second reviewer reads only that package's diff and lists every
reason, trade-off, invariant or warning that disappeared without a
replacement. Each one is restored before the commit.

## Order of work

One branch, cut from `main` **after PRs #34, #35 and #36 have merged** (PR #36
rewrote 42 comments in the usecase port files; starting earlier means
conflicts in the same lines). Item 2a (a timezone stored per household) waits
until item 0 has merged.

At the start, record the baseline: comment lines per package, split into
production and test, and the history-reference count. On `main` at `dfb00f2`
it was:

| Package | Production comment lines | Test comment lines |
|---|---|---|
| `internal/domain` | 1,437 | 496 |
| `internal/usecase` | 4,607 | 3,230 |
| `internal/adapter/http` | 2,570 | 2,747 |
| `internal/adapter/postgres` (excl. `sqlcgen`) | 1,689 | 1,847 |
| `internal/adapter/telegram` | 197 | 100 |
| other adapters (`clock`, `crypto`, `fx`, `intent`, `mail`, `openrouter`) | 252 | 73 |
| `internal/testsupport` | 21 | — |
| `cmd` | 546 | 169 |
| `queries/*.sql` | 1,096 (222 of them `-- name:`) | — |

Commits, one per package, each verified by the tool before it is made:

1. `domain`
2. `usecase` services (non-port, non-test files)
3. `usecase` ports (`ports*.go`)
4. `usecase` tests, doubles excluded
5. `usecase` test doubles
6. `adapter/http`
7. `adapter/postgres`, `queries/*.sql`, then `make sqlc`
8. `adapter/telegram` and the other adapters
9. `cmd` and `internal/testsupport`
10. The `lint-comments` guard and the `CLAUDE.md` Comments block
11. Docs: `docs/LEARNING.md` entry; this spec and the plan committed

Packages share no files, so commits 1–9 can be prepared in parallel and
committed in order.

## Done means

- The tool is green on every commit from 1 to 9.
- `make lint && make test` is green on the final tree, and `lint-comments`
  has been seen to fail on a planted violation.
- History references (`decision N`, `Task N`, `milestone N`, `file:line`) in
  `api/**/*.go` and `queries/*.sql`: **0**.
- The PR reports the before and after comment counts per package.
- A browser smoke test on `make dev`: sign in, open Budget, Bills and
  Portfolio. The expected result is that nothing has changed.
- `docs/LEARNING.md` has an entry: spec decision numbers copied into code go
  stale and cost every reader a file lookup; a lint check now stops them.
  If an existing pattern fits, it goes there as evidence.
- `docs/FEATURE_TRACKER.md` gets no row (a refactor ships no feature).
  `docs/SYSTEM_DESIGN.md` does not change (nothing structural moves).
- The untracked ranked list gains an item 0 row and a log line, and item 14's
  text drops the history-comment part it no longer owns.
- One PR, not pushed until the owner says so.

## Risks

| Risk | Guard |
|---|---|
| A code token changes by accident | The token comparison, per commit. |
| A tool-read comment is lost | The directive comparison, per commit. |
| A useful *why* is deleted | The per-package "lost why" review. |
| The diff is too large to review | Per-package commits; the tool means only the wording needs reading. |
| New history references appear later | `lint-comments` in `make lint`. |
| The branch conflicts with open PRs | Start only after #34–#36 merge; item 2a waits for item 0. |
