# Readability item 0 — backend comment sweep — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Trim every comment in the Go backend and its SQL queries to the
"Moderate" level: every *why* kept once and short, every history reference
replaced by the rule it stands for. Prove mechanically that no code changed,
and add a lint guard so history references cannot come back.

**Architecture:** Nothing in the product changes. The work is one branch with
one commit per package. A throwaway checker (`commentdiff`, source below)
compares each package against the branch's base commit: code tokens with
comments skipped must be identical, tool-read comments must be identical, no
exported identifier may lose its doc comment, and SQL outside comments must be
identical. A second reviewer per package checks what the checker cannot see:
a deleted *why*, a rewritten rule that is not true of the code, a pointer that
now points at nothing.

**Tech Stack:** Go 1.25 (`go/scanner`, `go/parser`), sqlc via `go tool sqlc`,
GNU Make, bash, `gh`.

**Spec:** `docs/superpowers/specs/2026-09-27-readability-comment-sweep-design.md`
(read it first; this plan argues from it).

## Global Constraints

- **No code changes.** Only comments in `.go` and `api/internal/adapter/postgres/queries/*.sql`, plus exactly six test failure messages (Task 4 and Task 6 list them). Production string literals never change.
- Out of scope, never edited: `api/migrations/`, `web/`, file names, identifiers, signatures, file moves or splits, moving `usecase/seed.go`, renaming `MailOutbox`.
- Tool-read comments are never edited: `//go:build`, `// +build`, `//go:embed`, `//go:generate`, `//go:linkname`, `//export`, `//line`, `//nolint`, `// Output:`, `// Unordered output:`, and every `-- name:` line in SQL.
- `sqlcgen/*.sql.go` is never edited by hand. Only `make sqlc` writes it.
- Every exported identifier that has a doc comment keeps one, and it starts with the identifier's name.
- Keep `ADR N` and `LEARNING pattern N` references. Remove `decision N`, `spec decision N`, `Decision N`, `Task N`, `task N`, `milestone N`, `Milestone N`, and `file.go:123`-style line references.
- Trim level is **Moderate** (the owner's choice): keep every *why*, trade-off, invariant, security reason and ordering reason, once, in as few lines as it takes.
- Comment lines wrap at about 80 columns, like the surrounding code.
- Precondition: PRs #34, #35 and #36 are merged into `main` before Task 0 starts.
- Nothing is pushed. The branch ends with a PR description written to a local file, and the owner says when to push.
- Explanations in commit messages and the PR description are plain and simple, for a junior engineer (`CLAUDE.md`).

## Review Focus

1. **A *why* deleted as if it were history.** A paragraph reads as a story ("before this, X happened…") but is the only record of why the current code is shaped this way. Expected: the reason survives, rewritten as "Keep X: without it, Y." Pinned by the reviewer prompt's check (a) in every sweep task.
2. **A rewritten rule that is not true of the code.** Replacing "(decision 22)" with a sentence means someone wrote a claim. Expected: every new rule sentence matches what the code does (LEARNING pattern 16). Pinned by reviewer check (b), which requires reading the code the comment sits on.
3. **A pointer that now points at nothing.** Another file says "see the comment on `BillService.List`", and the sweep shortened that comment. Expected: every "see …", "above", "below" and "the comment on X" still finds text that says what the pointer promises. Item 1 found seven of these. Pinned by reviewer check (c) and by Task 11's repository-wide grep.
4. **sqlc output drifting from its source.** A hand edit to `sqlcgen/`, or a regeneration that changes more than comments. Expected: `sqlcgen` changes only through `make sqlc`, a second `make sqlc` produces no diff, and `commentdiff` passes on `sqlcgen`. Pinned in Task 7.
5. **A history reference the lint guard can't see.** Plan-step references ("Step 6's mutation check") and history inside strings aren't caught by `lint-comments`, which deliberately skips "step N" because most uses are local numbered steps. Expected: the trimmers remove plan-step references by judgement. Pinned by Task 11's grep for `[Ss]tep [0-9]` with a manual read of every hit.

---

## Shared definitions

Every task uses these. Read this section before any task.

### Paths

- `$REPO` — the repository root (`/Volumes/Oink_Machine/Intelij/HouseholdDashboard` on the owner's machine).
- `$WORK` — your session scratchpad directory. Everything throwaway lives here.
- `$BASE` — `$WORK/base`, a detached git worktree at the branch's base commit (created in Task 0).
- `$CDIFF` — `$WORK/cdiff`, the built checker (Task 0).

Shell setup for every task (run in bash, not zsh: zsh does not split words in `$VAR` commands):

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
REPO=/Volumes/Oink_Machine/Intelij/HouseholdDashboard
WORK=<your scratchpad directory>
BASE=$WORK/base
CDIFF=$WORK/cdiff
```

### The comment rules (give these to every trimmer word for word)

```text
You are trimming comments in Go (and SQL) files of the Hearth backend. You
change COMMENTS ONLY. Never change code, identifiers, string literals, blank
lines between declarations, imports, or file names. Never edit these
tool-read comments: //go:build, // +build, //go:embed, //go:generate,
//go:linkname, //export, //line, //nolint, // Output:, // Unordered output:,
and SQL "-- name:" lines.

The level is MODERATE: keep every "why", said once and short.

| Comment kind | Action |
|---|---|
| History reference: "decision N", "spec decision N", "Task N", "task N", "milestone N", "Step N" of a plan | Replace the reference with the rule it stands for. If you cannot tell the rule from the code, open the spec (docs/superpowers/specs/, matched by feature: agreements -> 2026-09-05-hearth-agreements-design.md, bills -> 2026-08-09-hearth-bills-design.md, and so on) or the plan (docs/superpowers/plans/) and read the numbered decision. When the reasoning is too long to restate, name the spec FILE once, never a decision number. |
| History narration: earlier shapes, "once X ships", "was X, now Y", "before this change" | Delete. Keep it ONLY when someone could plausibly reintroduce the mistake, and then write it as a warning: "Don't X: Y." |
| Restates the code | Delete. |
| "file.go:123" line reference | Replace with the symbol, e.g. domain.Bill.DueIn. |
| Doc comment on an exported identifier | ALWAYS keep one, starting with the identifier's name. Trim it to the contract: what it does, its errors, its invariants. |
| A why, trade-off, invariant, security or ordering reason | Keep, once, in as few lines as it takes. |
| "ADR N", "LEARNING pattern N" | Keep as they are. |
| Test comments | The test name states the behaviour. A body comment stays only when the setup is not obvious (why THIS data). |
| Test doubles | Trim fully, but keep EVERY note saying where a double behaves differently from Postgres. |
| Numbered steps inside the same function ("// 1. lock", "step 1's lock") | Keep: they are local structure, not plan history. |

Worked example (usecase.BudgetPersonView, 11 lines to 5):

BEFORE
// BudgetPersonView is one row of Spending by person. Every membership that
// paid for something this month gets a row, and so does the unattributed
// bucket (MembershipID "") for spend with no payer on file -- a hand-entered
// transaction saved without one, or (once Bills ships) a bill with no "Paid
// by". That bucket's Name is deliberately "": copy for it belongs in the
// frontend (BudgetByPerson.tsx), the same house rule budgetCopy.ts follows
// for everything else this screen renders, not composed here. Dropping the
// bucket instead -- the earlier shape -- let the rows sum to less than Spent
// with nothing on screen explaining the gap; this is not the "Kids (shared)"
// grouping the spec rejects, since it attributes spend to nobody, it only
// names the absence of a payer.

AFTER
// BudgetPersonView is one row of "Spending by person".
//
// MembershipID "" collects spend with no payer on file. Keep that row:
// without it the rows sum to less than Spent and nothing on screen explains
// the gap. Its Name is empty on purpose - the frontend owns all copy here.

Second example, a history reference replaced by its rule:

BEFORE  // requireTwoOwners is decision 1's gate, and it runs FIRST in every write --
AFTER   // requireTwoOwners runs FIRST in every write: an agreement needs two
        // owners to mean anything, so a one-owner household gets no drafts --

Do not invent. If you cannot establish what a numbered decision said, keep
the substance of the sentence, drop only the number, and list the spot in
your report as UNSURE.

Wrap comment lines at about 80 columns. Run `gofmt -w` on every Go file you
touched. Do not run git commands.

Report when done: each file touched, how many comment lines before and after
(grep -cE '^\s*//' before/after), every UNSURE spot as file + symbol, and any
comment you deliberately left long and why.
```

### The reviewer prompt (the "lost why" review, one per sweep task)

```text
You are reviewing a comments-only diff in the Hearth backend. A tool has
already proven that no code changed. Your job is what the tool cannot see.
Get the diff with: git -C <REPO> diff <BASE_SHA> -- <PATHS>

For every hunk, check:
(a) LOST WHY: was a reason, trade-off, invariant, security property, ordering
    reason, or "Postgres behaves differently" note deleted without the same
    meaning surviving in the new text? Quote the deleted sentence.
(b) FALSE RULE: every sentence that replaced a "decision N" / "Task N" /
    "milestone N" reference states a rule. Open the code the comment sits
    on and confirm the rule is true of that code. Quote any that is not, and
    say what the code actually does.
(c) DANGLING POINTER: for every comment whose explanation shrank, grep the
    whole repository for comments pointing at it ("see <Symbol>", "the
    comment on <Symbol>", "<file> explains", "above", "below"). Report any
    pointer that now promises text that is no longer there.
(d) MISSING DOC: an exported identifier's doc comment must still start with
    its name.
(e) LEFTOVER HISTORY: any plan-step reference ("Step 6's mutation check",
    "the task 6 walk") still present.

Report a numbered list of findings, each with file, symbol, the category
letter, and the exact text to restore or fix. If there are none, say "no
findings" and list the 5 hunks you checked most carefully and why they are
fine.
```

### The sweep procedure (Tasks 1 to 9 run it on their own file set)

1. List the task's files and split them into groups of at most 2,500 lines, never splitting a file:

   ```bash
   group_files() { # stdin: one path per line; prints one group per line
     xargs wc -l | grep -v ' total$' | sort -k2 | awk '
       BEGIN { g = 0 }   # without this, the first group is keyed "" and printed empty
       { if (sum + $1 > 2500 && sum > 0) { g++; sum = 0 }
         sum += $1; files[g] = files[g] " " $2 }
       END { for (i = 0; i <= g; i++) print "group " i+1 ":" files[i] }'
   }
   ```

   Check the groups cover every file before dispatching: the number of paths across all groups must equal the number of input files.

2. Dispatch one trimmer per group, in parallel (fresh subagent, prompt = "The comment rules" above + "Your files:" + the group). Groups share no files, so parallel edits cannot collide. If you are executing inline, work group by group yourself.
3. Check only the task's paths changed:

   ```bash
   git -C $REPO status --short -- api | grep -vE '<TASK PATH REGEX>' && echo "OUT OF SCOPE EDIT" || echo "scope ok"
   ```

4. Format, build and vet:

   ```bash
   cd $REPO/api && test -z "$(gofmt -l .)" && go build ./... && go vet ./...
   ```

   The whole tree was gofmt-clean at planning time, so any file listed was touched by this task. (Never call `gofmt -l` with an empty file list: it waits on stdin.)

5. Run the checker against the base, cumulatively (earlier tasks' files show as changed too, and must still pass):

   ```bash
   $CDIFF <FLAGS> $BASE/api $REPO/api
   ```

   Expected last line: `... 0 failures`. Any `FAIL` line means a trimmer touched code: restore that file's code from `$BASE` and redo its comments.
6. No history reference is left in the task's files:

   ```bash
   REF='([Dd]ecision [0-9]|[Tt]ask [0-9]|[Mm]ilestone [0-9]|\.(go|sql|ts|tsx):[0-9])'
   grep -nE "//.*$REF" <TASK FILES> ; echo "exit=$? (1 means none left)"
   ```

7. Dispatch the reviewer (fresh subagent, prompt = "The reviewer prompt" with `<REPO>`, `<BASE_SHA>` from `$WORK/base-sha` and `<PATHS>` filled in). Fix every finding. Re-run steps 4–6 after the fixes.
8. Run the task's tests (named in each task).
9. Commit only the task's paths, with the message given in the task.

---

### Task 0: Branch, checker, baseline

**Files:**
- Create (throwaway, not committed): `$WORK/commentdiff/main.go`, `$WORK/count-comments.sh`, `$WORK/base-sha`, `$WORK/baseline.txt`, worktree `$BASE`

**Interfaces:**
- Produces: `$CDIFF [-allow-test-strings] <base-api-dir> <new-api-dir>`, which prints `FAIL …` lines and `TEST STRING …` blocks and ends with `checked N .go and M .sql files; C changed; S test strings changed; F failures`. Exit 1 if F > 0. Also `$WORK/count-comments.sh` (run from `$REPO`) and `$BASE_SHA` in `$WORK/base-sha`.

- [ ] **Step 1: Check the precondition**

```bash
cd $REPO && for n in 34 35 36; do gh pr view $n --json number,state -q '"\(.number) \(.state)"'; done
```

Expected: `34 MERGED`, `35 MERGED`, `36 MERGED`. If any is not merged, **stop and tell the owner**. This is the one real blocker: starting early means conflicts in the same comment lines.

- [ ] **Step 2: Create the branch and the base worktree**

```bash
cd $REPO && git switch main && git pull --ff-only && git switch -c refactor/comment-sweep
git rev-parse HEAD > $WORK/base-sha
git worktree add --detach $BASE $(cat $WORK/base-sha)
ls $REPO/api/internal/usecase/ports_*.go | head -3   # confirms #36's split is present
```

The spec and this plan are untracked files. They stay in the working tree across `git switch` and are committed in Task 11.

- [ ] **Step 3: Write the checker**

Write `$WORK/commentdiff/main.go` with exactly this content:

```go
// commentdiff proves that two copies of the Hearth api/ tree differ only in
// comments. Throwaway tool for the readability comment sweep (item 0).
//
// Usage: go run main.go [-allow-test-strings] <base-api-dir> <new-api-dir>
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var directive = regexp.MustCompile(`^//(go:|line |export |nolint|\s*\+build)|^//\s*(Unordered output|Output):`)

type tok struct {
	kind token.Token
	lit  string
	line int
}

func main() {
	allowTestStrings := flag.Bool("allow-test-strings", false, "string literals in _test.go files may differ (each one is printed)")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: commentdiff [-allow-test-strings] <base-api-dir> <new-api-dir>")
		os.Exit(2)
	}
	base, next := flag.Arg(0), flag.Arg(1)
	failures := 0
	fail := func(format string, a ...any) { failures++; fmt.Printf("FAIL "+format+"\n", a...) }

	baseGo, nextGo := list(base, isGo), list(next, isGo)
	for _, f := range diffSets(baseGo, nextGo) {
		fail("%s: file added or removed", f)
	}
	changed, stringsChanged := 0, 0
	for _, rel := range baseGo {
		if !contains(nextGo, rel) {
			continue
		}
		a, b := read(base, rel), read(next, rel)
		if a == b {
			continue
		}
		changed++
		ta, da := scan(a, strings.Contains(rel, "/sqlcgen/"))
		tb, db := scan(b, strings.Contains(rel, "/sqlcgen/"))
		if strings.Join(da, "\n") != strings.Join(db, "\n") {
			fail("%s: tool-read comments differ: %q vs %q", rel, da, db)
		}
		if !strings.HasSuffix(rel, "_test.go") && !strings.Contains(rel, "/sqlcgen/") {
			after := documented(b)
			for _, name := range documented(a) {
				if !contains(after, name) {
					fail("%s: exported %s lost its doc comment", rel, name)
				}
			}
		}
		if len(ta) != len(tb) {
			fail("%s: token count %d vs %d", rel, len(ta), len(tb))
			continue
		}
		for i := range ta {
			x, y := ta[i], tb[i]
			if x.kind == y.kind && x.lit == y.lit {
				continue
			}
			if x.kind == token.STRING && y.kind == token.STRING && *allowTestStrings && strings.HasSuffix(rel, "_test.go") {
				stringsChanged++
				fmt.Printf("TEST STRING %s:%d\n  - %s\n  + %s\n", rel, y.line, x.lit, y.lit)
				continue
			}
			fail("%s: token differs at base line %d / new line %d: %s %q vs %s %q", rel, x.line, y.line, x.kind, clip(x.lit), y.kind, clip(y.lit))
			break
		}
	}

	baseSQL, nextSQL := list(base, isQuerySQL), list(next, isQuerySQL)
	for _, f := range diffSets(baseSQL, nextSQL) {
		fail("%s: file added or removed", f)
	}
	for _, rel := range baseSQL {
		if !contains(nextSQL, rel) {
			continue
		}
		a, b := read(base, rel), read(next, rel)
		if a == b {
			continue
		}
		changed++
		if sqlCode(a) != sqlCode(b) {
			fail("%s: SQL outside comments differs (or a -- name: line changed)", rel)
		}
	}

	fmt.Printf("checked %d .go and %d .sql files; %d changed; %d test strings changed; %d failures\n",
		len(baseGo), len(baseSQL), changed, stringsChanged, failures)
	if failures > 0 {
		os.Exit(1)
	}
}

// scan returns the file's tokens without comments, plus its tool-read
// comments in order. In sqlcgen files every string literal loses its SQL
// comment lines first, because sqlc copies in-query comments into them.
func scan(src string, sqlcgen bool) ([]tok, []string) {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)
	var toks []tok
	var directives []string
	for {
		pos, kind, lit := s.Scan()
		if kind == token.EOF {
			return toks, directives
		}
		if kind == token.COMMENT {
			if directive.MatchString(lit) {
				directives = append(directives, lit)
			}
			continue
		}
		if kind == token.SEMICOLON && lit == "\n" {
			lit = ";" // an auto-inserted semicolon; where the newline falls is layout
		}
		if kind == token.STRING && sqlcgen {
			lit = sqlCode(lit)
		}
		toks = append(toks, tok{kind, lit, fset.Position(pos).Line})
	}
}

// documented lists the exported top-level names in a file that carry a doc
// comment: functions, methods (as Recv.Name), types, consts and vars.
func documented(src string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	if err != nil {
		fmt.Println("FAIL parse:", err)
		os.Exit(1)
	}
	var out []string
	add := func(name string, docs ...*ast.CommentGroup) {
		for _, d := range docs {
			if d != nil && ast.IsExported(name[strings.LastIndex(name, ".")+1:]) {
				out = append(out, name)
				return
			}
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) == 1 {
				t := d.Recv.List[0].Type
				if star, ok := t.(*ast.StarExpr); ok {
					t = star.X
				}
				if idx, ok := t.(*ast.IndexExpr); ok {
					t = idx.X
				}
				if id, ok := t.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			add(name, d.Doc)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					add(sp.Name.Name, sp.Doc, d.Doc)
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						add(n.Name, sp.Doc, d.Doc)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// sqlCode drops whole-line "--" comments (keeping "-- name:" lines, which sqlc
// reads) and blank lines, so only what Postgres executes is compared.
func sqlCode(src string) string {
	var keep []string
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || (strings.HasPrefix(t, "--") && !strings.HasPrefix(t, "-- name:")) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func isGo(rel string) bool { return strings.HasSuffix(rel, ".go") }

func isQuerySQL(rel string) bool {
	return strings.HasPrefix(rel, "internal/adapter/postgres/queries/") && strings.HasSuffix(rel, ".sql")
}

func list(root string, keep func(string) bool) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if !d.IsDir() && keep(filepath.ToSlash(rel)) {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func read(root, rel string) string {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		fmt.Println("FAIL", err)
		os.Exit(1)
	}
	return string(b)
}

func contains(xs []string, x string) bool {
	i := sort.SearchStrings(xs, x)
	return i < len(xs) && xs[i] == x
}

func diffSets(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !contains(b, x) {
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !contains(a, x) {
			out = append(out, x)
		}
	}
	return out
}
```

Build it:

```bash
cd $WORK/commentdiff && gofmt -l main.go && go vet main.go && go build -o $CDIFF main.go && echo built
```

Expected: `built`, with no gofmt or vet output.

- [ ] **Step 4: Prove the checker passes an untouched tree**

```bash
$CDIFF $BASE/api $REPO/api | tail -1
```

Expected: `checked … files; 0 changed; 0 test strings changed; 0 failures`.

- [ ] **Step 5: Prove the checker can fail: one planted change per check, each restored**

Run each block; each must print the `FAIL` shown, and the restore must bring the last line back to `0 failures`.

```bash
cd $REPO/api
# (1) a comment edit alone passes
perl -0pi -e 's|^// |// PLANTED |m' internal/domain/money.go
$CDIFF $BASE/api $REPO/api | tail -1          # expect: 1 changed; ... 0 failures
# (2) an identifier change fails
sed -i '' 's/^package domain/package domainx/' internal/domain/money.go
$CDIFF $BASE/api $REPO/api | grep FAIL        # expect: token differs ... IDENT "domain" vs IDENT "domainx"
git checkout -- internal/domain/money.go
# (3) a production string change fails even with the test-string allowance
f=internal/usecase/invite.go; perl -0pi -e 's/"context"/"contextX"/' $f
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep FAIL   # expect: STRING "\"context\"" vs ...
git checkout -- $f
# (4) a test string change fails without the flag, is listed with it
f=internal/usecase/invite_test.go; perl -pi -e 's/\(spec decision 8: 24 hours\)/(PLANTED)/' $f
$CDIFF $BASE/api $REPO/api | grep FAIL                        # expect: token differs ... STRING
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep -A2 'TEST STRING'   # expect the - / + pair
git checkout -- $f
# (5) a tool-read comment added fails
perl -0pi -e 's/^package domain/\/\/go:build linux\n\npackage domain/m' internal/domain/money.go
$CDIFF $BASE/api $REPO/api | grep FAIL        # expect: tool-read comments differ
git checkout -- internal/domain/money.go
# (6) removing an exported doc comment fails
python3 - internal/usecase/converter.go <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
m = re.search(r"((?://[^\n]*\n)+)(func (?:\([^)]*\) )?[A-Z])", s)
open(p, "w").write(s[:m.start(1)] + s[m.start(2):])
PY
$CDIFF $BASE/api $REPO/api | grep FAIL        # expect: exported <Name> lost its doc comment
git checkout -- internal/usecase/converter.go
# (7) a SQL change outside comments fails, and so does a -- name: change
f=internal/adapter/postgres/queries/account.sql
perl -pi -e 's/-- name: GetAccount :one/-- name: GetAccount :many/' $f
$CDIFF $BASE/api $REPO/api | grep FAIL        # expect: SQL outside comments differs
git checkout -- $f
# (8) an added file fails
touch internal/domain/planted.go
$CDIFF $BASE/api $REPO/api | grep FAIL        # expect: file added or removed
rm internal/domain/planted.go
$CDIFF $BASE/api $REPO/api | tail -1          # expect: 0 changed; ... 0 failures
git -C $REPO status --short -- api            # expect: empty
```

If any expected `FAIL` does not appear, the checker is broken. Fix it before any sweep task runs: a checker that cannot fail proves nothing (LEARNING pattern 2).

- [ ] **Step 6: Record the baseline**

Write `$WORK/count-comments.sh` with exactly this content:

```bash
#!/usr/bin/env bash
# Prints comment lines per package (production / test) and history references.
# Run from the repository root. Throwaway, for the item 0 PR description.
set -euo pipefail
cd api
count() { # $1 = find arguments selecting files
  local files; files=$(eval "find $1" | sort)
  [ -z "$files" ] && { echo 0; return; }
  echo "$files" | xargs cat | grep -cE '^\s*//' || true
}
printf '%-44s %8s %8s\n' package production test
for d in internal/domain internal/usecase internal/adapter/http internal/adapter/postgres internal/adapter/telegram \
         internal/adapter/clock internal/adapter/crypto internal/adapter/fx internal/adapter/intent \
         internal/adapter/mail internal/adapter/openrouter internal/testsupport cmd; do
  p=$(count "$d -name '*.go' ! -name '*_test.go' ! -path '*/sqlcgen/*'")
  t=$(count "$d -name '*_test.go'")
  printf '%-44s %8s %8s\n' "$d" "$p" "$t"
done
printf '%-44s %8s\n' 'queries/*.sql (comment lines, incl -- name:)' \
  "$(cat internal/adapter/postgres/queries/*.sql | grep -cE '^\s*--')"
REF='([Dd]ecision [0-9]|[Tt]ask [0-9]|[Mm]ilestone [0-9]|\.(go|sql|ts|tsx):[0-9])'
printf '%-44s %8s\n' 'history references in comments' \
  "$( { grep -rnE --include='*.go' "//.*$REF" . ; grep -rnE -e "--.*$REF" internal/adapter/postgres/queries internal/adapter/postgres/sqlcgen; } | wc -l | tr -d ' ')"
```

```bash
chmod +x $WORK/count-comments.sh && cd $REPO && $WORK/count-comments.sh | tee $WORK/baseline.txt
```

Expected: close to the spec's table (on `dfb00f2` it was domain 1437/496, usecase 4607/3230, http 2570/2747, postgres 1689/1847, and 450 history references). PRs #34–#36 move these slightly. The recorded file is the baseline, whatever it says.

No commit in this task.

---

### Task 1: `domain`

**Files:**
- Modify: `api/internal/domain/*.go` (production and tests)

**Interfaces:**
- Consumes: `$CDIFF`, `$BASE`, `$WORK/base-sha` (Task 0).
- Produces: commit 1.

- [ ] **Step 1: Run the sweep procedure** on `ls api/internal/domain/*.go` with scope regex `api/internal/domain/`, checker flags none.
- [ ] **Step 2: Test**

```bash
cd $REPO/api && go test ./internal/domain/... -count=1
```

Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
cd $REPO && git add api/internal/domain && git commit -m "Comments in domain state the rule, not the plan number

Comments only. commentdiff proves the code tokens are identical to the
branch base; a reviewer checked no reason was lost."
```

---

### Task 2: `usecase` services

**Files:**
- Modify: every `api/internal/usecase/*.go` that is not `ports*.go` and not `*_test.go` (for example `bill.go`, `budget.go`, `agreement.go`, `seed.go`)

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 2.

- [ ] **Step 1: Run the sweep procedure** on

```bash
ls api/internal/usecase/*.go | grep -v '_test\.go$' | grep -v '/ports[^/]*\.go$'
```

with scope regex `api/internal/usecase/[^/]*\.go` (and step 3 must show no `ports*.go` or `_test.go` file), checker flags none.

- [ ] **Step 2: Test**

```bash
cd $REPO/api && go test ./internal/usecase/... -count=1
```

Expected: `ok` (the usecase tests use in-memory doubles and need no Docker).

- [ ] **Step 3: Commit**

```bash
cd $REPO && git add $(git diff --name-only -- api/internal/usecase | grep -v '_test\.go$' | grep -v '/ports[^/]*\.go$') \
  && git commit -m "Comments in usecase services state the rule, not the plan number

Comments only. commentdiff proves the code tokens are identical to the
branch base; a reviewer checked no reason was lost."
```

---

### Task 3: `usecase` ports

**Files:**
- Modify: `api/internal/usecase/ports.go` and every `api/internal/usecase/ports_*.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 3.

The port doc comments are the contract between the layers (`CLAUDE.md`: "load-bearing"). Tell each trimmer in this task, in addition to the rules: *every error a method may return, every "must"/"never" an implementation has to honour, and every "a caller may rely on" guarantee stays.* The reviewer's check (a) applies with that in mind.

- [ ] **Step 1: Run the sweep procedure** on `ls api/internal/usecase/ports*.go`, scope regex `api/internal/usecase/ports[^/]*\.go`, checker flags none.
- [ ] **Step 2: Test**

```bash
cd $REPO/api && go test ./internal/usecase/... -count=1 && go doc ./internal/usecase | head -5
```

Expected: `ok`, and `go doc` prints the package description.

- [ ] **Step 3: Commit**

```bash
cd $REPO && git add api/internal/usecase/ports*.go && git commit -m "Port contracts: keep every guarantee, drop the plan numbers

Comments only. Every returned error and every must/never a repository
has to honour is kept. commentdiff proves the code tokens are identical."
```

---

### Task 4: `usecase` tests (doubles excluded)

**Files:**
- Modify: every `api/internal/usecase/*_test.go` except `testdouble_test.go` and `holding_double_test.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 4, including four test failure messages.

Four failure messages in this task's files cite a decision number. They are the only string literals that may change here. Tell the trimmer of each group that holds one of them to rewrite it exactly as below:

| File | Before | After |
|---|---|---|
| `agreement_test.go` | `"Status = %q, want parked -- Discuss leaves the proposal open (decision 7)"` | `"Status = %q, want parked -- Discuss leaves the proposal open"` |
| `invite_test.go` | `"expires at %v, want %v (spec decision 8: 24 hours)"` | `"expires at %v, want %v (an invite lives 24 hours)"` |
| `invite_test.go` | `"role is %q; Let in grants exactly what the invite said (spec decision 14)"` | `"role is %q; Let in grants exactly what the invite said"` |
| `telegram_auth_test.go` | `"got %q, want decision 15's own sentence"` | `"got %q, want the already-belongs-to-a-household sentence"` |

- [ ] **Step 1: Run the sweep procedure** on

```bash
ls api/internal/usecase/*_test.go | grep -vE '/(testdouble|holding_double)_test\.go$'
```

with scope regex `api/internal/usecase/[^/]*_test\.go`, checker flags `-allow-test-strings`.

- [ ] **Step 2: Check the string list**

```bash
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep '^TEST STRING'
```

Expected: exactly four lines, for the three files above (two in `invite_test.go`). Any other file means a trimmer changed a string it should not have: restore it.

- [ ] **Step 3: Test**

```bash
cd $REPO/api && go test ./internal/usecase/... -count=1
```

Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
cd $REPO && git add $(git diff --name-only -- api/internal/usecase | grep '_test\.go$' | grep -vE '/(testdouble|holding_double)_test\.go$') \
  && git commit -m "usecase tests: comments and four failure messages state the rule

Comments, plus four t.Fatalf/t.Errorf messages that cited a decision
number. commentdiff proves nothing else changed."
```

---

### Task 5: `usecase` test doubles

**Files:**
- Modify: `api/internal/usecase/testdouble_test.go` (about 5,000 lines), `api/internal/usecase/holding_double_test.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 5.

`testdouble_test.go` is larger than one group, but it is a single file, so it gets a single trimmer (never split a file between two writers). Tell that trimmer, in addition to the rules: *item 12 will pin these doubles to Postgres with a contract suite, and it needs every note that says where a double does something Postgres does not, or skips something Postgres does. Keep all of them. Also remove the stale "Task 6" references (follow-up F12).*

- [ ] **Step 1: Run the sweep procedure** on those two files, scope regex `api/internal/usecase/(testdouble|holding_double)_test\.go`, checker flags `-allow-test-strings` (the flag is needed only because Task 4's strings are cumulative).
- [ ] **Step 2: Check no new string changed**

```bash
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep '^TEST STRING' | grep -cvE '(agreement|invite|telegram_auth)_test\.go'
```

Expected: `0`.

- [ ] **Step 3: Test**

```bash
cd $REPO/api && go test ./internal/usecase/... -count=1
```

- [ ] **Step 4: Commit**

```bash
cd $REPO && git add api/internal/usecase/testdouble_test.go api/internal/usecase/holding_double_test.go \
  && git commit -m "Test doubles: shorter comments, every Postgres difference kept

Comments only. Each note on where a double behaves unlike Postgres is kept
for item 12. commentdiff proves the code tokens are identical."
```

---

### Task 6: `adapter/http`

**Files:**
- Modify: `api/internal/adapter/http/*.go` (production and tests; `errors.go`, `router.go`, all handlers, middleware, and the API tests)

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 6, including two test failure messages.

Tell the trimmers of `router.go` and the `middleware_*.go` files, in addition to the rules: *authorisation lives only in this layer (ADR 8), so every comment that explains why a guard is on a route, why guards are in this order, or why a route is deliberately unguarded stays.*

The two failure messages allowed to change in this task:

| File | Before | After |
|---|---|---|
| `budget_api_test.go` | `"(Task 8's 'Over requires an actual line' decision)"` | `"(Over requires an actual cap line)"` |
| `holdings_api_test.go` | `"notInNetWorth = false; milestone 1 keeps holdings out of net worth and the page says so"` | `"notInNetWorth = false; holdings stay out of net worth and the page says so"` |

- [ ] **Step 1: Run the sweep procedure** on `ls api/internal/adapter/http/*.go`, scope regex `api/internal/adapter/http/`, checker flags `-allow-test-strings`.
- [ ] **Step 2: Check the string list**

```bash
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep '^TEST STRING'
```

Expected: exactly six lines: Task 4's four plus the two above.

- [ ] **Step 3: Test** (needs Docker)

```bash
cd $REPO/api && go test ./internal/adapter/http/... -count=1 -timeout=20m
```

Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
cd $REPO && git add api/internal/adapter/http && git commit -m "Comments in the HTTP layer state the rule, not the plan number

Comments, plus two test failure messages that cited a task or milestone.
Every reason a guard sits where it does is kept (ADR 8). commentdiff
proves nothing else changed."
```

---

### Task 7: `adapter/postgres`, the SQL queries, and sqlc

**Files:**
- Modify: `api/internal/adapter/postgres/*.go` (production and tests), `api/internal/adapter/postgres/queries/*.sql`
- Regenerated (never hand-edited): `api/internal/adapter/postgres/sqlcgen/*.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 7.

Tell the trimmers of the `.sql` files, in addition to the rules: *a comment above a `-- name:` line becomes the Go doc comment of the generated method; a comment inside a query becomes part of the query string. Never touch a `-- name:` line or any SQL. Keep every comment that explains a lock (`FOR UPDATE`, `FOR SHARE`), an index the query relies on, or why a filter exists.* Tell the trimmers of the repo `.go` files: *keep every comment about which transaction a call runs in, which lock it takes, and how a Postgres error is translated.*

- [ ] **Step 1: Run the sweep procedure** on

```bash
{ ls api/internal/adapter/postgres/*.go; ls api/internal/adapter/postgres/queries/*.sql; }
```

with scope regex `api/internal/adapter/postgres/`, checker flags `-allow-test-strings`. Skip the procedure's step 4 gofmt for `.sql` files (the pipeline already filters to `.go`).

- [ ] **Step 2: Regenerate sqlc, twice**

```bash
cd $REPO && make sqlc && git diff --stat -- api/internal/adapter/postgres/sqlcgen | tee $WORK/sqlc-1.txt | tail -1
make sqlc && git diff --stat -- api/internal/adapter/postgres/sqlcgen | diff - $WORK/sqlc-1.txt && echo "stable"
```

Expected: some `sqlcgen` files changed after the first run, and `stable` (a second run changes nothing more).

- [ ] **Step 3: Re-run the checker and the history grep over sqlcgen**

```bash
$CDIFF -allow-test-strings $BASE/api $REPO/api | tail -1
REF='([Dd]ecision [0-9]|[Tt]ask [0-9]|[Mm]ilestone [0-9]|\.(go|sql|ts|tsx):[0-9])'
grep -rnE -e "--.*$REF" -e "//.*$REF" api/internal/adapter/postgres/sqlcgen api/internal/adapter/postgres/queries; echo "exit=$? (1 means none left)"
```

Expected: `0 failures`, and `exit=1`.

- [ ] **Step 4: Test** (needs Docker)

```bash
cd $REPO/api && go test ./internal/adapter/postgres/... -count=1 -timeout=20m
```

- [ ] **Step 5: Commit**

```bash
cd $REPO && git add api/internal/adapter/postgres && git commit -m "Comments in the Postgres adapter and SQL queries state the rule

Comments only; sqlcgen regenerated by make sqlc, not edited. commentdiff
proves the Go tokens and the executed SQL are identical to the branch base."
```

---

### Task 8: `adapter/telegram` and the other adapters

**Files:**
- Modify: `api/internal/adapter/{telegram,clock,crypto,fx,intent,mail,openrouter}/**/*.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 8.

Tell the `crypto` trimmer, in addition to the rules: *security-sensitive code; keep every comment that explains a constant-time comparison, a hash choice, a token length or an encoding.*

- [ ] **Step 1: Run the sweep procedure** on

```bash
find api/internal/adapter/telegram api/internal/adapter/clock api/internal/adapter/crypto api/internal/adapter/fx \
     api/internal/adapter/intent api/internal/adapter/mail api/internal/adapter/openrouter -name '*.go' | sort
```

with scope regex `api/internal/adapter/(telegram|clock|crypto|fx|intent|mail|openrouter)/`, checker flags `-allow-test-strings`.

- [ ] **Step 2: Test**

```bash
cd $REPO/api && go test ./internal/adapter/telegram/... ./internal/adapter/clock/... ./internal/adapter/crypto/... \
  ./internal/adapter/fx/... ./internal/adapter/intent/... ./internal/adapter/mail/... ./internal/adapter/openrouter/... -count=1
```

- [ ] **Step 3: Commit**

```bash
cd $REPO && git add api/internal/adapter/telegram api/internal/adapter/clock api/internal/adapter/crypto \
  api/internal/adapter/fx api/internal/adapter/intent api/internal/adapter/mail api/internal/adapter/openrouter \
  && git commit -m "Comments in Telegram and the small adapters state the rule

Comments only. commentdiff proves the code tokens are identical."
```

---

### Task 9: `cmd` and `testsupport`

**Files:**
- Modify: `api/cmd/**/*.go` (`api`, `adminctl`, `hearthctl`, and any other command), `api/internal/testsupport/*.go`

**Interfaces:**
- Consumes: Task 0 outputs.
- Produces: commit 9.

Tell the `cmd/api/main.go` trimmer, in addition to the rules: *keep every comment that explains why a dependency is optional, why a background loop starts where it does, or why a config value has its default.* Tell the `cmd/hearthctl` trimmer: *`hearthctl` finds routes with a regex over the router source (item 7). Keep any comment that describes that coupling.*

- [ ] **Step 1: Run the sweep procedure** on `find api/cmd api/internal/testsupport -name '*.go' | sort`, scope regex `api/(cmd|internal/testsupport)/`, checker flags `-allow-test-strings`.
- [ ] **Step 2: Test**

```bash
cd $REPO/api && go test ./cmd/... -count=1 -timeout=20m
cd $REPO && make hearthctl
```

- [ ] **Step 3: Commit**

```bash
cd $REPO && git add api/cmd api/internal/testsupport && git commit -m "Comments in the commands and test support state the rule

Comments only. commentdiff proves the code tokens are identical."
```

---

### Task 10: The `lint-comments` guard and the `CLAUDE.md` rule

**Files:**
- Modify: `Makefile` (the `.PHONY` list at the top, the `lint:` target, a new `lint-comments` target placed after `lint-dead`)
- Modify: `CLAUDE.md` (a new bullet block in "Readable by a junior engineer in their first week")

**Interfaces:**
- Consumes: Tasks 1–9 (the guard passes only once every history reference is gone).
- Produces: `make lint-comments`, run by `make lint`. Commit 10.

- [ ] **Step 1: Add `lint-comments` to `.PHONY` and to `lint`**

In `Makefile`, add `lint-comments` to the `.PHONY` list next to `lint-dead`, and change

```make
lint: lint-arch typecheck lint-web lint-dead ## Run every linter
```

to

```make
lint: lint-arch typecheck lint-web lint-dead lint-comments ## Run every linter
```

- [ ] **Step 2: Add the target after `lint-dead`'s recipe**

```make
# A comment states the rule itself. A plan number ("decision 7", "Task 12",
# "milestone 2") sends the reader to a document to understand one line, and a
# line number ("bill.go:202") is wrong after the next edit above it. Only
# comments are searched: // in Go, and -- in the SQL queries and in sqlcgen,
# which copies those into its query strings. A product word in a string or a
# name is not history.
HISTORY_REF := ([Dd]ecision [0-9]|[Tt]ask [0-9]|[Mm]ilestone [0-9]|\.(go|sql|ts|tsx):[0-9])

lint-comments: ## Fail on plan numbers and line numbers in backend comments
	@out=$$( { grep -rnE --include='*.go' '//.*$(HISTORY_REF)' api; \
	           grep -rnE -e '--.*$(HISTORY_REF)' api/internal/adapter/postgres/queries api/internal/adapter/postgres/sqlcgen; } || true); \
	 if [ -n "$$out" ]; then echo "$$out"; echo "lint-comments: state the rule, not a plan number or a line number (CLAUDE.md, Comments)"; exit 1; fi; \
	 echo "lint-comments passed"
```

Recipe lines start with a real tab.

- [ ] **Step 3: Run it green, then prove it can fail**

```bash
cd $REPO && make lint-comments
```

Expected: `lint-comments passed`. If it lists lines, a sweep task missed them: fix those comments (and re-run `$CDIFF`), then continue.

```bash
echo '// see decision 4' >> api/internal/domain/money.go && make lint-comments; echo "exit=$?"
git checkout -- api/internal/domain/money.go
echo '-- see Task 3' >> api/internal/adapter/postgres/queries/account.sql && make lint-comments; echo "exit=$?"
git checkout -- api/internal/adapter/postgres/queries/account.sql
echo '// see bill.go:202' >> api/internal/domain/money.go && make lint-comments; echo "exit=$?"
git checkout -- api/internal/domain/money.go
make lint-comments
```

Expected: the three planted runs print the planted line and `exit=2` (make's code for a failed recipe). The last run prints `lint-comments passed`. (On this plan's dry run, the unswept tree failed with 450 hits, a clean copy passed, and one planted comment failed.)

- [ ] **Step 4: Add the `CLAUDE.md` rule**

In `CLAUDE.md`, under `### Readable by a junior engineer in their first week`, directly after the bullet that starts "Write every non-obvious decision down", insert:

```markdown
- **Comments state the rule, not where it came from.** Never cite a plan
  number ("decision 7", "Task 12", "milestone 2") or a line number
  ("bill.go:202"); write the rule itself, and name a symbol instead of a
  line. `make lint-comments` fails on both. `ADR N` and `LEARNING pattern N`
  are fine: they are lasting documents.
- Keep every *why* once and short. Delete comments that restate the code and
  stories of earlier shapes; keep a past mistake only as a warning someone
  needs ("Don't X: Y").
```

- [ ] **Step 5: Commit**

```bash
cd $REPO && git add Makefile CLAUDE.md && git commit -m "lint-comments: fail on plan and line numbers in backend comments

Specs number their decisions, and the code cited those numbers until a
reader needed the spec open to understand one line. make lint now fails on
them, and CLAUDE.md says what to write instead."
```

---

### Task 11: Final gate, browser smoke, docs, PR description

**Files:**
- Modify: `docs/LEARNING.md`
- Add (commit the untracked files): `docs/superpowers/specs/2026-09-27-readability-comment-sweep-design.md`, `docs/superpowers/plans/2026-09-27-readability-comment-sweep.md`
- Modify (untracked, never `git add`): `docs/reviews/2026-09-26-architecture-review.md`
- Create (throwaway): `$WORK/after.txt`, `$WORK/pr-body.md`

**Interfaces:**
- Consumes: commits 1–10, `$WORK/baseline.txt`, `$WORK/commentdiff/main.go`.
- Produces: commit 11 and a PR description file. Nothing pushed.

- [ ] **Step 1: Leftover checks across the whole tree**

```bash
cd $REPO
$CDIFF -allow-test-strings $BASE/api $REPO/api | tail -1                  # expect 0 failures
$CDIFF -allow-test-strings $BASE/api $REPO/api | grep -c '^TEST STRING'   # expect 6
grep -rnE --include='*.go' '//.*[Ss]tep [0-9]' api | grep -v sqlcgen      # read EVERY hit
grep -rnE --include='*.go' '//.*\b(see|above|below)\b' api | grep -v sqlcgen > $WORK/pointers.txt; wc -l < $WORK/pointers.txt
```

For the `Step N` grep: each hit must be a numbered step inside the same function (keep) and never a plan step (rewrite, then re-run `$CDIFF`). For `$WORK/pointers.txt`: for every line whose target is a symbol in a file this branch changed (`git diff --name-only $(cat $WORK/base-sha)`), open the target and confirm it still says what the pointer promises. Fix any that don't, in a follow-up commit on the owning package.

- [ ] **Step 2: Full gate**

```bash
cd $REPO && make lint && make test
```

Expected: every linter passes (including `lint-comments passed`, `deadcode passed`, `staticcheck passed`), every Go package `ok`, all frontend tests pass. Record the tail of each in `$WORK/gate.txt`.

- [ ] **Step 3: Browser smoke**

Nothing user-visible should change, and that is what this step checks. First confirm which Docker engine serves port 5173 (two engines can host the stack on this machine):

```bash
lsof -nP -iTCP:5173 -sTCP:LISTEN
cd $REPO && make dev    # in the background, if not already running
make seed               # prints the sign-in details
```

With the browser tools, open `http://localhost:5173`, sign in with the seeded owner, then open **Budget**, **Bills** and **Portfolio**. Each page must load with figures and no error banner. Check the browser console for errors. Record what you saw in `$WORK/smoke.txt`. Don't create data, so there's nothing to clean up.

- [ ] **Step 4: Count after**

```bash
cd $REPO && $WORK/count-comments.sh | tee $WORK/after.txt
paste -d'|' $WORK/baseline.txt $WORK/after.txt
```

Expected: history references `0`, and fewer comment lines in every package.

- [ ] **Step 5: `docs/LEARNING.md` entry**

Check whether item 1's entry (merged with #36) about stale pointers in comments sits under pattern 16 ("A claim about the code is not evidence until someone checks it against the code"). If it does, add this as evidence beside it. If not, add it under `## Catalogue by area` → `### Tooling and infrastructure`:

```markdown
- **Comments that cite a plan number go stale and cost every reader a
  lookup (2026-09-27, item 0).** 450 comments cited "decision N", "Task N",
  "milestone N" or a `file.go:123` line: agents copied the spec's decision
  numbers into the code, so understanding one line meant opening a spec, and
  line numbers were wrong after the next edit above them. Comments were 39%
  of `usecase` and 34% of `domain`. The sweep rewrote each reference as the
  rule it stood for, and proved the change comments-only with a token
  comparison against the branch base (code tokens, tool-read comments,
  exported doc comments, and SQL outside comments all identical, each check
  seen to fail on a planted change). What stops it recurring: `make
  lint-comments` fails on those references in any backend comment, and
  `CLAUDE.md` says to write the rule instead. What would have caught it
  sooner: a lint on day one. The references were only noise until the
  specs they pointed at stopped being the code's current truth.
```

Replace "450" and the percentages with the numbers from `$WORK/baseline.txt` if they differ.

- [ ] **Step 6: Update the untracked ranked list (never `git add` it)**

In `docs/reviews/2026-09-26-architecture-review.md`: set item 0's status to 🟡 with "branch `refactor/comment-sweep`, not pushed". In item 14's text, change "In every file an item above touches, rewrite comments that narrate history ("decision N" ×146, "Task N" ×64) so they state the rule and the trade-off. Replace line-number references with symbol names." to "History comments were done by item 0." Append a dated log line with the before/after counts and the gate result.

- [ ] **Step 7: Commit the docs, spec and plan**

```bash
cd $REPO && git add docs/LEARNING.md docs/superpowers/specs/2026-09-27-readability-comment-sweep-design.md \
  docs/superpowers/plans/2026-09-27-readability-comment-sweep.md \
  && git status --short | grep -v '^??' ; git commit -m "Docs: what the comment sweep taught, and its spec and plan"
git status --short -- docs/reviews   # must still show ?? (untracked)
```

- [ ] **Step 8: Write the PR description, and stop**

Write `$WORK/pr-body.md` with: what changed and why (two plain paragraphs); the before/after table from Step 4; the six test failure messages that changed (from the `TEST STRING` output); the gate result from `$WORK/gate.txt`; the smoke result from `$WORK/smoke.txt`; how to re-check it yourself ("check out the base commit into a worktree, build this tool, run `cdiff -allow-test-strings <base>/api <branch>/api`") followed by the full source of `$WORK/commentdiff/main.go` in a fenced block; and what is not in this PR (`seed.go` move, `MailOutbox` rename, both still on item 14).

Then remove the base worktree:

```bash
cd $REPO && git worktree remove $BASE
```

Tell the owner: branch `refactor/comment-sweep`, 11 commits, the before/after table, the gate result, and that the PR description is ready at `$WORK/pr-body.md`. **Don't push**: the owner says when.
