# Split `usecase/ports.go` by product slice — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the 2,104-line `api/internal/usecase/ports.go` into one file per product slice, and point every comment that says "see ... in ports.go" at the symbol instead — changing no behaviour.

**Architecture:** A pure move, done by a script, proven by three independent checks: every non-blank line of the old file appears exactly once in the new files (a line-multiset comparison), `go doc -all` of the package is unchanged apart from the package description, and the build, vet and tests pass. `ports.go` stays, holding only the package doc comment, which now lists every ports file — so anyone who follows an old "see ports.go" pointer lands on the index. Comment rewrites happen in a separate commit, so the move commit contains no edited lines at all.

**Tech Stack:** Go 1.25 (module `github.com/andreasoentoro/hearth/api`), Python 3 for the split tooling, sqlc for regenerating one query file's comments.

**Spec:** Item 1 of the architecture work queue (`docs/reviews/2026-09-26-architecture-review.md`, untracked on purpose — read it locally, never commit it): *"Split `usecase/ports.go` by product slice (money, identity, marriage, channels, admin, platform), and fix the stale code references found by the review. Changes no behaviour. Goes first because every later item edits `ports.go`, and small per-slice files make those diffs, and their merges, small."* The handover (`docs/reviews/2026-09-27-handover-next-agent.md`, also untracked) adds: *"While moving, fix stale `file.go:NNN` references in the moved comments by pointing at symbol names instead of line numbers. The only proof needed is that the build and the full gate stay green."*

## Global Constraints

- **No behaviour change.** No signature, type, method, constant or error value changes. Only file boundaries and comment text move.
- **Go is not on `PATH`:** `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH` before any `go` or `make`. Inside `api/`, `GOTOOLCHAIN=auto` fetches go1.25.13 by itself.
- **Stage by explicit path.** Never `git add -A` or `git add .`. Never stage anything in `docs/reviews/` — those files are untracked on purpose (the repository is public).
- **Do not edit history:** `docs/superpowers/plans/**`, `docs/superpowers/specs/**`, `.claude/plans/**`, `.claude/worktrees/**`, `docs/reviews/**`, `graft/**`, and existing `docs/LEARNING.md` entries describe the past and keep saying `ports.go`. Only live code comments and live guidance docs change.
- **Never hand-edit `api/internal/adapter/postgres/sqlcgen/`.** Edit `queries/*.sql` and run `make sqlc`.
- Branch: `refactor/split-usecase-ports`, from `main`.
- Commit messages end with your own session's `Claude-Session:` line.
- CLAUDE.md's Definition of done applies: `make lint && make test` green, `docs/LEARNING.md` updated, `docs/SYSTEM_DESIGN.md` kept true (use the `maintaining-system-design` skill), browser smoke check.

## Review Focus

1. **A declaration, or its doc comment, silently dropped.** The dry run of this plan's own script lost `Clock` (the first declaration) to an off-by-one before it was fixed; only the `go doc` comparison caught it. Task 1 runs both the line-multiset verifier and the `go doc` diff, and Step 4 proves the verifier can fail.
2. **A floating section comment detached from what it describes.** `ports.go` lines 430–437 ("The platform admin ports. …") belong to no declaration. It must sit directly above `PlatformAdminRepository` at the top of `ports_admin.go`. Task 1 Step 7 checks it.
3. **A second package doc comment.** Each new file starts with a one-line description; if it touched `package usecase` with no blank line between, `go doc` would glue it into the package description. Task 1 Step 6 checks the package description appears once.
4. **Generated code drifting from its SQL.** Two comments live in `queries/bill.sql` and are copied into `sqlcgen/bill.sql.go`. Task 2 edits the SQL, runs `make sqlc`, and checks the generated diff is those two comments only.
5. **A rewritten comment that now points at the wrong symbol.** Task 2 gives the exact replacement for every reference, each checked against the port that owns it; Step 5 greps for leftovers.

---

## File map

| New file | Holds |
|---|---|
| `ports.go` | Package doc comment and the index of ports files. No declarations. |
| `ports_platform.go` | `Clock`, `PasswordHasher`, `TokenGenerator`, `Mailer` |
| `ports_identity.go` | `StoredUser`, `UserRepository`, `SessionRecord`, `SessionRepository`, `MagicLinkRepository`, `APITokenRepository`, `LoginAttemptRepository` |
| `ports_identity_household.go` | `HouseholdRepository`, `MemberView`, `MembershipRepository`, `SpaceRepository`, `NotificationPreferences`, `NotificationRepository` |
| `ports_identity_invite.go` | `InviteDetails`, `AcceptedInvite`, `AdmittedMember`, `AdmittedInvite`, `InviteKnock`, `InviteSummary`, `PairingCodes`, `InviteRepository` |
| `ports_identity_signup.go` | `SignupDetails`, `ProvisionedHousehold`, `SignupRepository` |
| `ports_channel.go` | `TelegramSender`, `TelegramLinkRedemption`, `TelegramLinkRequest`, `TelegramLinkRepository`, `TelegramBinding`, `TelegramAccountRepository`, `InviteKnocker`, `InviteChats`, `NudgeRecipient`, `NudgeRepository` |
| `ports_admin.go` | the platform-admin section comment, `PlatformAdminRepository` … `PendingInvite` (directory types, `MemberChannel` and its constants), `DatabaseBrowser`, `TableInfo`, `ColumnInfo`, `RowPage`, `ErrBrowseUnavailable`, `ErrOutboxUnavailable`, `MailOutbox`, `OutboxPage`, `OutboxMessage` |
| `ports_money.go` | `FXRateProvider`, `AccountView`, `AccountMonthMovement`, `AccountRepository`, `AccountLookup`, `TransactionView`, `TransactionFilter`, `CategoryRepository`, `CategoryLookup`, `TransactionRepository`, `RollOverToGoalInput`, `BudgetRepository` |
| `ports_money_goal.go` | `GoalRecord`, `GoalMonthTotal`, `GoalLookup`, `GoalRepository` |
| `ports_money_bill.go` | `BillRecord`, `BillPaymentRecord`, `NewBillRow`, `PaymentWrite`, `BillRepository` |
| `ports_money_holding.go` | `HoldingRecord`, `HoldingRepository`, `HoldingCounter`, `HoldingEventRepository`, `HoldingValuationRepository`, `HoldingIncomeRepository` |
| `ports_marriage_retro.go` | `RetroRecord`, `RetroSummary`, `RetroActionInput`, `RetroActionRecord`, `RetroUpdate`, `RetroRepository`, `RetroActionRepository` |
| `ports_marriage_vision.go` | `GoalProgress`, `GoalProgressReader`, `VisionRepository` |
| `ports_marriage_agreement.go` | `AgreementSectionRecord`, `AgreementRecord`, `AgreementProposalRecord`, `AgreementDocument`, `AgreementProposalWrite`, `AgreementSignatureWrite`, `AgreementRepository` |

Why these boundaries (write them down so nobody re-litigates them):

- **Per slice, then per feature where a slice is large.** Six files, one per slice, would leave `ports_money.go` at about 1,000 lines — the problem this item exists to fix. The `ports_<slice>_<feature>.go` names keep each slice's files next to each other in a directory listing.
- **`GoalProgress` and `GoalProgressReader` go with vision, not goals.** VisionService declares that one-method reader for itself (interface segregation); the port belongs with its consumer, and its own doc comment says so.
- **`InviteKnocker` and `InviteChats` go with the channel slice.** They are the hand-off between the Telegram bot and InviteService; both doc comments describe them from the bot's side.
- **`MemberChannel`, `HouseholdMember` and `PendingInvite` go with admin.** They are the operator directory's own view types (`HouseholdDetail` uses them), not the household's.
- Sizes after the split: 36–303 lines per file.

---

### Task 1: Move the declarations (pure move, no edited lines)

**Files:**
- Modify: `api/internal/usecase/ports.go` (becomes the index)
- Create: the 14 `api/internal/usecase/ports_*.go` files in the file map
- Scratch (never committed): `declspan/main.go`, `split_ports.py`, `verify_split.py` in your session's scratch directory, referred to below as `$SCRATCH`

**Interfaces:**
- Consumes: nothing.
- Produces: the same exported API of package `usecase`, byte for byte, spread over the files in the file map. Task 2 relies on the file names in the file map.

- [ ] **Step 1: Branch and record the baseline**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git switch main && git pull --ff-only
git switch -c refactor/split-usecase-ports
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
SCRATCH=<your session scratch directory>
cp api/internal/usecase/ports.go $SCRATCH/ports.orig.go
(cd api && go doc -all ./internal/usecase) > $SCRATCH/doc-before.txt
```

- [ ] **Step 2: Write the declaration-span tool**

It prints one line per top-level declaration: first line (doc comment included), last line, names. Save as `$SCRATCH/declspan/main.go`, with a `go.mod` beside it containing `module declspan` and `go 1.24`:

```go
// declspan prints one line per top-level declaration in a Go file:
// "<first line> <last line> <names>", where first line includes the doc comment.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	for _, d := range f.Decls {
		start := d.Pos()
		var names []string
		switch g := d.(type) {
		case *ast.GenDecl:
			if g.Tok == token.IMPORT {
				continue
			}
			if g.Doc != nil {
				start = g.Doc.Pos()
			}
			for _, s := range g.Specs {
				switch sp := s.(type) {
				case *ast.TypeSpec:
					names = append(names, sp.Name.Name)
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						names = append(names, n.Name)
					}
				}
			}
		case *ast.FuncDecl:
			if g.Doc != nil {
				start = g.Doc.Pos()
			}
			names = append(names, g.Name.Name)
		}
		fmt.Printf("%d %d %s\n", fset.Position(start).Line, fset.Position(d.End()).Line, strings.Join(names, ","))
	}
}
```

Run it:

```bash
(cd $SCRATCH/declspan && go run . /Volumes/Oink_Machine/Intelij/HouseholdDashboard/api/internal/usecase/ports.go) > $SCRATCH/spans.txt
wc -l $SCRATCH/spans.txt
```

Expected: `107` lines, the first `13 15 Clock`, the last `2088 2104 HoldingIncomeRepository`. If `ports.go` on `main` has changed since 2026-09-27 (a merged PR touched it), the numbers differ; that is fine, but any **new** declaration name must be added to `FILES` in Step 3, or the split script stops with `no destination for <Name>`.

- [ ] **Step 3: Write the split script**

Save as `$SCRATCH/split_ports.py`:

```python
#!/usr/bin/env python3
"""Split api/internal/usecase/ports.go into ports_<slice>.go files.

Pure move: every line of every declaration, doc comments and floating
section comments included, lands in exactly one new file, in its original
order. Run from the repository root:

    python3 split_ports.py <spans.txt>

spans.txt is declspan's output for ports.go: "<first> <last> <names>".
"""
import re, sys, textwrap

SRC = "api/internal/usecase/ports.go"
DIR = "api/internal/usecase/"

# Destination for every top-level declaration, keyed by its first name.
# A name missing here stops the script: nothing is ever dropped silently.
FILES = {
    "ports_platform.go": ("the platform ports: time, hashing, tokens and outbound mail -- "
                          "the infrastructure every slice leans on",
        ["Clock", "PasswordHasher", "TokenGenerator", "Mailer"]),
    "ports_identity.go": ("the identity slice's ports: users, sessions, magic links, "
                          "personal API tokens and the sign-in lockout ledger",
        ["StoredUser", "UserRepository", "SessionRecord", "SessionRepository",
         "MagicLinkRepository", "APITokenRepository", "LoginAttemptRepository"]),
    "ports_identity_household.go": ("the identity slice's household ports: the household "
                                    "row, memberships, spaces and notification preferences",
        ["HouseholdRepository", "MemberView", "MembershipRepository", "SpaceRepository",
         "NotificationPreferences", "NotificationRepository"]),
    "ports_identity_invite.go": ("the identity slice's invite ports: inviting a member, "
                                 "the Telegram knock, and the pairing code",
        ["InviteDetails", "AcceptedInvite", "AdmittedMember", "AdmittedInvite", "InviteKnock",
         "InviteSummary", "PairingCodes", "InviteRepository"]),
    "ports_identity_signup.go": ("the identity slice's self-serve sign-up ports",
        ["SignupDetails", "ProvisionedHousehold", "SignupRepository"]),
    "ports_channel.go": ("the channel slice's ports: Telegram sending, account linking, "
                         "the invite hand-offs between the bot and InviteService, and nudges",
        ["TelegramSender", "TelegramLinkRedemption", "TelegramLinkRequest",
         "TelegramLinkRepository", "TelegramBinding", "TelegramAccountRepository",
         "InviteKnocker", "InviteChats", "NudgeRecipient", "NudgeRepository"]),
    "ports_admin.go": ("the platform-admin slice's ports: admins, feature flags, the audit "
                       "log, re-authentication, the household directory, the database "
                       "browse and the outbound mail inspector",
        ["PlatformAdminRepository", "PlatformAdminListing", "FeatureFlagRepository",
         "HouseholdFlagOverride", "AdminAuditEntry", "AdminAuditRepository",
         "AdminReauthAttemptRepository", "AdminDirectoryRepository", "DirectoryMetrics",
         "HouseholdListing", "MemberMatch", "HouseholdDetail", "MemberChannel",
         "ChannelEmail", "HouseholdMember", "PendingInvite", "DatabaseBrowser", "TableInfo",
         "ColumnInfo", "RowPage", "ErrBrowseUnavailable", "ErrOutboxUnavailable",
         "MailOutbox", "OutboxPage", "OutboxMessage"]),
    "ports_money.go": ("the money slice's core ports: exchange rates, accounts, "
                       "transactions, categories and budgets",
        ["FXRateProvider", "AccountView", "AccountMonthMovement", "AccountRepository",
         "AccountLookup", "TransactionView", "TransactionFilter", "CategoryRepository",
         "CategoryLookup", "TransactionRepository", "RollOverToGoalInput",
         "BudgetRepository"]),
    "ports_money_goal.go": ("the money slice's goal ports",
        ["GoalRecord", "GoalMonthTotal", "GoalLookup", "GoalRepository"]),
    "ports_money_bill.go": ("the money slice's bill ports",
        ["BillRecord", "BillPaymentRecord", "NewBillRow", "PaymentWrite", "BillRepository"]),
    "ports_money_holding.go": ("the money slice's investment-holding ports",
        ["HoldingRecord", "HoldingRepository", "HoldingCounter", "HoldingEventRepository",
         "HoldingValuationRepository", "HoldingIncomeRepository"]),
    "ports_marriage_retro.go": ("the marriage slice's monthly retro ports",
        ["RetroRecord", "RetroSummary", "RetroActionInput", "RetroActionRecord",
         "RetroUpdate", "RetroRepository", "RetroActionRepository"]),
    "ports_marriage_vision.go": ("the marriage slice's vision ports, including the "
                                 "one-method goal reader VisionService declares for itself",
        ["GoalProgress", "GoalProgressReader", "VisionRepository"]),
    "ports_marriage_agreement.go": ("the marriage slice's agreement ports",
        ["AgreementSectionRecord", "AgreementRecord", "AgreementProposalRecord",
         "AgreementDocument", "AgreementProposalWrite", "AgreementSignatureWrite",
         "AgreementRepository"]),
}

PACKAGE_DOC = """// Package usecase holds the application services. It depends on domain and on
// the port interfaces declared in its ports files — never on an adapter.
//
// The ports are the contract between the layers, and their doc comments are
// load-bearing: read a port's comment before writing a service that calls it
// or a repository that implements it. They live in one file per product
// slice, so a change to one area's contract is a small diff in one file:
//
"""

def main():
    src = open(SRC).read().split("\n")
    owner = {}
    for f, (_, names) in FILES.items():
        for n in names:
            assert n not in owner, f"{n} mapped twice"
            owner[n] = f
    spans = []
    for line in open(sys.argv[1]):
        a, b, names = line.split(" ", 2)
        first = names.strip().split(",")[0]
        assert first in owner, f"no destination for {first}"
        spans.append((int(a), int(b), first))
    used = {s[2] for s in spans}
    assert used == set(owner), f"mapped but not in ports.go: {set(owner) - used}"

    chunks = {f: [] for f in FILES}
    # The first ")" line after "import (" closes the import block; the
    # first declaration starts after it.
    imp = src.index("import (")
    prev_end = src.index(")", imp) + 1  # 1-based line number of that ")"
    for a, b, first in spans:
        # Everything since the previous declaration -- a floating section
        # comment included -- travels with this one.
        body = src[prev_end:b]  # lines prev_end+1 .. b, 1-based
        while body and not body[0].strip():
            body.pop(0)
        chunks[owner[first]].append("\n".join(body))
        prev_end = b

    index = []
    for f, (desc, _) in FILES.items():
        text = "\n\n".join(chunks[f])
        # Imports are decided from code only: a doc comment that mentions
        # domain.Money must not pull in an import nothing uses.
        code = "\n".join(l.split("//")[0] for l in text.split("\n"))
        imports = [p for p, pat in (("context", r"\bcontext\."), ("errors", r"\berrors\."),
                                    ("time", r"\btime\."))
                   if re.search(pat, code)]
        block = "\n".join(f'\t"{p}"' for p in imports)
        if re.search(r"\bdomain\.", code):
            block += ("\n\n" if block else "") + '\t"github.com/andreasoentoro/hearth/api/internal/domain"'
        head = "\n".join(textwrap.wrap(f"This file holds {desc}. ports.go lists every ports file.",
                                     width=76, initial_indent="// ", subsequent_indent="// "))
        head += "\n\npackage usecase\n\n"
        if block:
            head += f"import (\n{block}\n)\n\n"
        open(DIR + f, "w").write(head + text + "\n")
        index.append("\n".join(textwrap.wrap(f"{f}: {desc}", width=74,
                                              initial_indent="//   - ",
                                              subsequent_indent="//     ")))

    open(SRC, "w").write(PACKAGE_DOC + "\n".join(index) + "\npackage usecase\n")

main()
```

- [ ] **Step 4: Write the verifier, and prove it can fail**

Save as `$SCRATCH/verify_split.py`:

```python
#!/usr/bin/env python3
"""Prove the split moved lines and changed none: the multiset of non-blank
lines after the import block in the old ports.go must equal the multiset of
non-blank lines after the import block (or package clause) across the new
ports_*.go files. Run from the repository root:

    python3 verify_split.py <path to the ORIGINAL ports.go>
"""
import collections, glob, sys

def body(path):
    lines = open(path).read().split("\n")
    if "import (" in lines:
        start = lines.index(")", lines.index("import (")) + 1
    else:
        start = lines.index("package usecase") + 1
    return [l for l in lines[start:] if l.strip()]

before = collections.Counter(body(sys.argv[1]))
after = collections.Counter()
for f in sorted(glob.glob("api/internal/usecase/ports_*.go")):
    after.update(body(f))
missing, extra = before - after, after - before
for l, n in missing.items():
    print(f"MISSING x{n}: {l}")
for l, n in extra.items():
    print(f"EXTRA x{n}: {l}")
print(f"{sum(before.values())} lines before, {sum(after.values())} after")
sys.exit(1 if missing or extra else 0)
```

- [ ] **Step 5: Run the split and the verifier**

```bash
python3 $SCRATCH/split_ports.py $SCRATCH/spans.txt
python3 $SCRATCH/verify_split.py $SCRATCH/ports.orig.go
```

Expected: `1981 lines before, 1981 after` and exit 0 (the count changes only if `ports.go` changed on `main`; before and after must still match).

Now prove the verifier catches a loss — delete one line, run it, restore:

```bash
cp api/internal/usecase/ports_platform.go $SCRATCH/pp.bak
sed -i '' '/Now() time.Time/d' api/internal/usecase/ports_platform.go
python3 $SCRATCH/verify_split.py $SCRATCH/ports.orig.go; echo "exit $?"
cp $SCRATCH/pp.bak api/internal/usecase/ports_platform.go
```

Expected: `MISSING x1: 	Now() time.Time` and `exit 1`. Then re-run the verifier once more: exit 0.

- [ ] **Step 6: Build, vet, format, compare the API, run the tests**

```bash
cd api
gofmt -l internal/usecase                 # expected: no output
go build ./... && go vet ./...            # expected: no output
go doc -all ./internal/usecase > $SCRATCH/doc-after.txt
diff $SCRATCH/doc-before.txt $SCRATCH/doc-after.txt | grep '^<'
go test ./internal/usecase ./internal/domain -count=1
cd ..
```

Expected from the `diff`: exactly one removed line, `< port interfaces declared here — never on an adapter.` (the package description's old wording). Every other `diff` line starts with `>` and is inside the package description (the new index). **Any other `<` line is a lost declaration — stop and find it.** Also check `head -5 $SCRATCH/doc-after.txt` shows the package description once, starting "Package usecase holds the application services".

- [ ] **Step 7: Check the floating section comment**

```bash
sed -n 1,20p api/internal/usecase/ports_admin.go
```

Expected: after the file's own description and the import block, the first text is `// The platform admin ports. Platform admin is an axis orthogonal to`, then a blank line, then `type PlatformAdminRepository interface {`.

- [ ] **Step 8: Commit the move**

```bash
git add api/internal/usecase/ports.go api/internal/usecase/ports_*.go
git status --short | grep -v '^??'     # expected: 1 modified, 14 added, nothing else
git commit -m "Split usecase/ports.go into one file per product slice

A pure move: every line of every declaration lands in exactly one new
file, in its original order. ports.go keeps the package doc comment, which
now lists every ports file. Verified by a line-multiset comparison
(1981 non-blank lines before and after) and by go doc -all, whose only
change is the package description.

Claude-Session: <your session URL>"
```

---

### Task 2: Point every "see ports.go" comment at its symbol

**Files:** every file below; `api/internal/adapter/postgres/sqlcgen/bill.sql.go` changes only through `make sqlc`.

**Interfaces:**
- Consumes: the file map from Task 1 (only to know that "ports.go" no longer holds declarations).
- Produces: comments that name a symbol and no file, so the next move cannot make them stale.

The rule: **name the symbol, not the file.** Where the comment already names the symbol, delete the file mention. Where it does not, name it. Reflow a comment paragraph to keep lines under about 80 columns if the edit leaves a line very short or long; change no other words.

- [ ] **Step 1: The two stale "Task 12" references inside the moved files**

These name a task number from an old plan, which means nothing to a reader now.

| File | Replace | With |
|---|---|---|
| `api/internal/usecase/ports_identity.go` (`StoredUser` doc comment) | `and Task 12 already treats that` | `and AuthService.SignIn already treats that` |
| `api/internal/usecase/ports_money.go` (`TransactionRepository.List`) | `so a caller (Task 12's handler) that passes through an unvalidated` | `so a caller (the GET /transactions handler) that passes through an unvalidated` |

`AuthService.SignIn` is the method that refuses an empty hash (`auth.go`, `if user.PasswordHash == ""`).

- [ ] **Step 2: Go comments in the adapters, domain and usecase**

| File (line on `main` at `dfb00f2`) | Replace | With |
|---|---|---|
| `adapter/postgres/loginattempt_repo.go:52-53` | `See LoginAttemptRepository.Prune's doc` / `comment in ports.go for the` | `See usecase.LoginAttemptRepository.Prune's` / `doc comment for the` |
| `adapter/postgres/retro_action_repo.go:154` | `SetDone's own contract (usecase/ports.go):` | `RetroActionRepository.SetDone's own contract:` |
| `adapter/postgres/invite_repo.go:482-483` | `TelegramAccountRepository.Create's own contract` / `(ports.go) deliberately keeps` | `TelegramAccountRepository.Create's own contract` / `deliberately keeps` |
| `adapter/postgres/retro_action_repo_test.go:195` | `ForRetro's own contract (usecase/ports.go, and 00009_retros.sql's comment` | `RetroActionRepository.ForRetro's own contract (and 00009_retros.sql's comment` |
| `adapter/postgres/bill_repo.go:104` | `(ports.go's own doc comment)` | `(BillRepository.Update's own doc comment)` |
| `adapter/postgres/user_repo_test.go:12` | `which ports.go does not` | `which StoredUser's doc comment does not` |
| `adapter/postgres/retro_repo_test.go:160` | `Create's own contract (usecase/ports.go):` | `RetroRepository.Create's own contract:` |
| `adapter/postgres/convert.go:28` | `even though ports.go's doc comment` | `even though StoredUser's doc comment` |
| `adapter/postgres/convert.go:294-297` | the comment `Compile-time confirmation that every repository satisfies its port. Nothing in internal/usecase constructs these yet -- that is Task 12's job -- so without this, a signature drift from ports.go would not surface until then.` | `Compile-time confirmation that every repository satisfies its port: a signature drift from a usecase port fails the build here, in the adapter that drifted, rather than at whichever caller first wires it.` |
| `adapter/postgres/loginattempt_repo_test.go:11` | `the one method ports.go` / `singles out` | `the one method LoginAttemptRepository's doc` / `comment singles out` |
| `adapter/postgres/translate.go:29` | `CategoryRepository's own contract (usecase/ports.go) wants` | `CategoryRepository's own contract wants` |
| `adapter/postgres/household_repo.go:50` | `HouseholdRepository.Create's doc comment in ports.go.` | `usecase.HouseholdRepository.Create's doc comment.` |
| `adapter/postgres/admin_directory_repo_test.go:115` | `(per` / `ports.go's own contract on HouseholdListing.Match)` | `(per` / `HouseholdListing.Match's own doc comment)` |
| `adapter/http/middleware_session.go:140-141` | `(see its doc comment in` / `ports.go)` | `(see` / `usecase.MembershipRepository.ByUser's doc comment)` |
| `adapter/http/transaction_handlers.go:32` | `(see its doc comment in usecase/ports.go)` | `(see its doc comment in package usecase)` |
| `adapter/http/retro_handlers.go:17` | `(RetroUpdate's own doc comment in ports.go)` | `(usecase.RetroUpdate's own doc comment)` |
| `adapter/http/errors.go:58-59` | `(RetroUpdate's own` / `doc comment in ports.go)` | `(usecase.RetroUpdate's` / `own doc comment)` |
| `adapter/http/bills_api_test.go:276` | `(ports.go's own comment)` | `(usecase.BillRecord's own doc comment)` |
| `adapter/http/pending_invite_handlers.go:32` | `(InviteSummary vs PendingInvite; see ports.go)` | `(InviteSummary vs PendingInvite; see InviteSummary's doc comment)` |
| `adapter/http/bill_handlers.go:320` | `(its own doc comment in ports.go)` | `(its own doc comment)` |
| `usecase/invite.go:55` | `(see both ports' doc comments in ports.go for why` | `(see both ports' doc comments for why` |
| `usecase/invite.go:162` | `see its doc comment in ports.go for why` | `see UserRepository.CreateWithMembership's doc comment for why` |
| `usecase/invite.go:441-442` | `(see InviteRepository.Accept's doc` / `comment in ports.go).` | `(see InviteRepository.Accept's doc` / `comment).` |
| `usecase/bill.go:524-525` | `(PaidByMembershipID's own comment` / `in ports.go)` | `(domain.Bill.PaidByMembershipID:` / `"" when unattributed)` |
| `usecase/signup.go:249-250` | `see CreateConsumed's doc` / `comment in ports.go --` | `see SignupRepository.CreateConsumed's` / `doc comment --` |
| `usecase/retro_test.go:327-328` | `RetroUpdate.Month's own doc comment in` / `ports.go explains why` | `RetroUpdate.Month's own doc comment` / `explains why` |
| `usecase/testdouble_test.go:752-753` | `(see its doc comment` / `in ports.go)` | `(see InviteRepository.Accept's` / `doc comment)` |
| `usecase/testdouble_test.go:1153-1154` | `(see its doc comment in` / `ports.go)` | `(see SignupRepository.Provision's` / `doc comment)` |
| `usecase/testdouble_test.go:1328` | `CreateForTelegram's own doc comment in ports.go says why` | `SignupRepository.CreateForTelegram's doc comment says why` |
| `usecase/testdouble_test.go:1415` | `(see Provision's doc comment in ports.go)` | `(see SignupRepository.Provision's doc comment)` |
| `usecase/testdouble_test.go:1533-1534` | `(see Mailer's doc comments in` / `ports.go for why)` | `(see Mailer's doc comments` / `for why)` |
| `usecase/testdouble_test.go:1641` | `(SendSignupForExistingAccount's doc comment in ports.go)` | `(Mailer.SendSignupForExistingAccount's doc comment)` |
| `usecase/testdouble_test.go:2809-2813` | `not decoration: nothing in internal/usecase constructs a GoalService yet` / `(that is Task 6's job), so without this assertion a signature drift` / `between this double and ports.go's GoalRepository would not surface until` / `then --` | `not decoration: without this assertion a signature drift between this` / `double and GoalRepository would surface only when a test first builds a` / `GoalService with it --` |
| `usecase/testdouble_test.go:3794` | `(see its doc comment in ports.go and` | `(see TelegramLinkRepository.Consume's doc comment and` |
| `usecase/testdouble_test.go:4293` | `(see ports.go's doc comment)` | `(see InviteKnocker's doc comment)` |
| `domain/errors.go:274-275` | `(BillRecord's own` / `comment in ports.go)` | `(usecase.BillRecord's` / `own doc comment)` |
| `domain/bill.go:75` | `// "" when uncategorised, the ports.go NULL convention` | `// "" when uncategorised, the "" <-> SQL NULL convention` |

All paths are under `api/internal/`. Line numbers are from `main` at `dfb00f2` and may have shifted by a few lines; find each by its quoted text.

- [ ] **Step 3: The SQL source, then regenerate**

| File | Replace | With |
|---|---|---|
| `api/internal/adapter/postgres/queries/bill.sql:2-3` | `the ports.go` / `-- NULL convention)` | `the "" <-> SQL` / `-- NULL convention)` |
| `api/internal/adapter/postgres/queries/bill.sql:70` | `(ports.go's Update comment)` | `(BillRepository.Update's doc comment)` |

```bash
make sqlc
git diff --stat api/internal/adapter/postgres/sqlcgen/
git diff api/internal/adapter/postgres/sqlcgen/ | grep '^[-+][^-+]'
```

Expected: only `bill.sql.go` changed, and the only changed lines are those two comments. Anything else means `make sqlc` generated from a different sqlc version or schema — stop and investigate before committing.

- [ ] **Step 4: The frontend comment**

| File | Replace | With |
|---|---|---|
| `web/src/features/marriage/retroCopy.ts:333-334` | `(RetroActionInput's own doc comment in` / `// ports.go, "the id of` | `(usecase.RetroActionInput's own doc` / `// comment, "the id of` |

- [ ] **Step 5: Check nothing is left, build, vet, test**

```bash
grep -rn 'ports\.go' api web/src --include='*.go' --include='*.sql' --include='*.ts' --include='*.tsx' \
  | grep -v '/sqlcgen/' | grep -v '^api/internal/usecase/ports'
```

Expected: no output. The `usecase/ports*.go` files themselves are filtered out on purpose: each new file's header says "ports.go lists every ports file", which points at the index and is correct.

```bash
cd api && gofmt -l ./internal && go build ./... && go vet ./... && go test ./internal/usecase ./internal/domain -count=1 && cd ..
cd web && npx tsc --noEmit && cd ..
```

Expected: no gofmt output; everything else passes.

- [ ] **Step 6: Commit**

```bash
git add <each file edited in Steps 1-4, by path> api/internal/adapter/postgres/sqlcgen/bill.sql.go
git status --short | grep -v '^??'     # only the files you meant
git commit -m "Point every \"see ports.go\" comment at its symbol

After the split, \"in ports.go\" names a file that no longer holds the
declaration. Each comment now names the port or type it means, which no
future move can make stale. Also retires the \"Task 6\" and \"Task 12\"
references, task numbers from old plans that mean nothing now.

Claude-Session: <your session URL>"
```

---

### Task 3: Guidance docs, LEARNING, gate, smoke check

**Files:**
- Modify: `CLAUDE.md`, `.claude/agents/hearth-architect.md`, `.claude/agents/hearth-engineer.md`, `.claude/skills/maintaining-system-design/SKILL.md`, `docs/HANDOVER.md`, `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`

**Interfaces:**
- Consumes: the file map from Task 1.
- Produces: docs a new engineer can follow to the right file.

- [ ] **Step 1: Live guidance docs** (use the `maintaining-system-design` skill for `SYSTEM_DESIGN.md`)

Same rule as Task 2: where the sentence already names the symbol, drop the file; name a file only where the sentence is about the files themselves.

| File:line (at `dfb00f2`) | Replace | With |
|---|---|---|
| `CLAUDE.md:122` | `` `internal/usecase/ports.go` is the contract between the layers. Its doc comments`` | `` The `internal/usecase/ports*.go` files are the contract between the layers (`ports.go` lists them, one per product slice). Their doc comments`` — then reflow the paragraph |
| `CLAUDE.md:149-150` | `` `usecase/ports.go` is`` / `the model.` | `` The `usecase/ports*.go` files are`` / `the model.` |
| `.claude/agents/hearth-architect.md:50` | `` `api/internal/usecase/ports.go` `` | `` `api/internal/usecase/ports*.go` (`ports.go` is the index) `` |
| `.claude/agents/hearth-engineer.md:37` | `` in `api/internal/usecase/ports.go` `` | `` in the matching `api/internal/usecase/ports_<slice>.go` `` |
| `.claude/agents/hearth-engineer.md:92` | `` `api/internal/usecase/ports.go` `` | `` `api/internal/usecase/ports*.go` (`ports.go` is the index) `` |
| `.claude/skills/maintaining-system-design/SKILL.md:49` | `` migration, `ports.go`. `` | `` migration, the `usecase/ports*.go` files. `` |
| `docs/HANDOVER.md:523` | `services + every port interface (ports.go)` | `services + every port interface (ports*.go)` |
| `docs/HANDOVER.md:539` | `` `internal/usecase/ports.go` is the contract. Read it `` | `` The `internal/usecase/ports*.go` files are the contract (`ports.go` lists them). Read the one for your slice `` — reflow |
| `docs/SYSTEM_DESIGN.md:692` | `subgraph usecase["internal/usecase/ — services + ports.go"]` | `subgraph usecase["internal/usecase/ — services + ports*.go"]` |
| `docs/SYSTEM_DESIGN.md:740` | `` declared in `usecase/ports.go` `` | `` declared in the `usecase/ports*.go` files `` |
| `docs/SYSTEM_DESIGN.md:833` | `` declared in `ports.go`, `` | `` a port declared by `usecase`, `` |
| `docs/SYSTEM_DESIGN.md:863` | `` `usecase/ports.go` is the contract between the layers. `` | `` The `usecase/ports*.go` files are the contract between the layers: one file per product slice, with `ports.go` holding the package doc comment that lists them. The split (2026-09) exists so a change to one slice's contract is a small diff in one file. `` |
| `docs/SYSTEM_DESIGN.md:909-910` | `` declared outside`` / `` `usecase/ports.go`, `` | `` declared outside`` / `` the `usecase/ports*.go` files, `` |
| `docs/SYSTEM_DESIGN.md:3031` | `` comment (`ports.go`) is where `` | `` comment is where `` |
| `docs/SYSTEM_DESIGN.md:4213` | `` comment in `usecase/ports.go` states `` | `` comment states `` |
| `docs/SYSTEM_DESIGN.md:4254` | `` doc comment in `usecase/ports.go` carries `` | `` doc comment carries `` |
| `docs/FEATURE_TRACKER.md:174` | `` (`api/internal/usecase/ports.go`) `` | `` (package `usecase`) `` |
| `docs/FEATURE_TRACKER.md:1151` | `` `usecase/ports.go` is`` | `` The `usecase/ports*.go` files are`` |
| `docs/FEATURE_TRACKER.md:1766` | `` (`ports.go`) `` | `` (its port's doc comment) `` |

No FEATURE_TRACKER row is added or changed: this is a refactor, not a feature. Say so in the PR.

Check:

```bash
grep -n 'ports\.go' CLAUDE.md .claude/agents/*.md .claude/skills/maintaining-system-design/SKILL.md docs/HANDOVER.md docs/SYSTEM_DESIGN.md docs/FEATURE_TRACKER.md
```

Expected: only lines that mention `ports.go` as the index (for example "`ports.go` lists them"), none that say a declaration lives in it.

- [ ] **Step 2: LEARNING.md**

Add under pattern 2 ("A test that cannot fail protects nothing") — the lesson is about the verification, not the move:

```markdown
- **A refactor's "no behaviour change" needs a check that could fail.**
  Splitting `usecase/ports.go` (2026-09) was done by a script, and the
  script's first dry run dropped `Clock`, the file's first declaration, to
  an off-by-one in where it thought the import block ended. The build would
  have failed loudly for `Clock`; it would not have for a dropped doc
  comment or a floating section comment, and those are what this file's
  readers depend on. Two checks caught or would catch every such loss: a
  line-multiset comparison of the old file against the new ones (1981
  non-blank lines before, 1981 after, proven able to fail by deleting a
  line), and `go doc -all` before and after, whose only permitted change was
  the package description. **For a move, compare the moved text, not only
  the compiled result.**
```

- [ ] **Step 3: Full gate**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
make lint && make test
```

Expected: architecture lint passed, every Go package `ok`, all frontend tests pass. The Postgres test package takes about 6 minutes and sometimes fails at start-up with `port "5432/tcp" not found`; that is a testcontainers flake — re-run that package once. If `make test` stops before the frontend, run `make test-web` separately.

- [ ] **Step 4: Browser smoke check**

Nothing user-visible changed, so this is a smoke check, not a walk. Confirm the API container rebuilt after your last edit (`docker logs --since 10m hearth-api-1 | grep -E 'building|listening'`), then in a real browser at http://localhost:5173: sign in as `andreas@hearth.family` / `hearth-dev-password`, open Overview, Finances, Transactions, Portfolio and Settings, and confirm each loads its data with no error banner.

- [ ] **Step 5: Commit, review, PR**

```bash
git add CLAUDE.md .claude/agents/hearth-architect.md .claude/agents/hearth-engineer.md .claude/skills/maintaining-system-design/SKILL.md docs/HANDOVER.md docs/SYSTEM_DESIGN.md docs/FEATURE_TRACKER.md docs/LEARNING.md docs/superpowers/plans/2026-09-27-split-usecase-ports.md
git status --short | grep -v '^??'
git commit -m "Docs: the usecase ports now live in one file per slice

Claude-Session: <your session URL>"
```

Then run the `code-review` skill against `main...refactor/split-usecase-ports`, fix what is in scope, record out-of-scope findings as new F-items in the (untracked) work queue, and update its Status column and Log for item 1. Ask the owner before `git push` and `gh pr create`.
