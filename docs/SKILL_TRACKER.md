# Hearth — skill and agent tracker

Project skills live in `.claude/skills/<name>/SKILL.md`. Each one exists because
a real defect got through in this project, or because a job needed an owner.

Project agents live in `.claude/agents/<name>.md` and are listed further down.
The difference matters: a **skill** loads instructions into the session you are
already in, so it changes how *you* work. An **agent** runs in its own context
and hands back a result, so it does a job *for* you and its conclusions arrive
as something to review.

Add a row here whenever you add either, and say plainly when to reach for it.

---

## The skills

| Skill | Use it when | Why it exists |
|---|---|---|
| **`hearth-product-driver`** | Anyone asks what to build next, whether something is already built, how much is left, or wants to start a new area. Also whenever a feature ships, to record it. | The roadmap needs an owner. Without one the feature tracker decays into fiction and the product gets built out of dependency order — Overview before Money means building Overview twice. |
| **`hunting-sibling-defects`** | Immediately after fixing any bug, before closing it. Also when a review finds a defect. | Fixing one instance failed to fix the class **six times** here. A PATCH corrected in two of three endpoints; an error oracle closed at the mailer and left two lines below; a non-awaited invalidation fixed in one panel with two siblings untouched; and `time.Truncate` — which operates on the absolute instant, not a calendar day in a location — shipped the identical misunderstanding at two sites on the accounts branch before anyone named it, with a third site correct but shipped untested. A further sibling of the same shape is found and still open, not fixed here: `member_handlers.go`'s email redaction blanks a field by name the same way the accounts redaction once did ([issue #1](https://github.com/oandrz/Household/issues/1)). |
| **`proving-tests-can-fail`** | Writing a test for a guard, a rule, an ordering, or a security property. Also when a suite passes on a change you expected to break something. | **Eleven** tests here passed against deliberately broken code. A fixture that agreed with the wrong answer, a stub that ignored the URL, a route-walk matrix that would have rejected every accounts write at the wrong guard and never reached the one it was written to test, two Finances assertions that only passed because mocked fetches happened to settle in the same microtask batch, a breakdown-ordering rule and an archived-account exclusion that both had nothing behind them until someone deleted the code they protect and watched the suite stay green, and a `Math.round` mutation that left the one test meant to catch it green outright. |
| **`verifying-in-the-real-environment`** | Work touches the DOM, a browser API, SQL, a transaction, a migration, or wiring you introduced. Before claiming a UI feature works. Also when a service answers an error it never logged, or a detail of the response — a request ID's hostname, a counter that should have reset — doesn't match the process you believe is running. | jsdom's `<dialog>` is a stub, so five green tests hid a modal that threw on **every open** in production — and fixing that exposed a second bug that had been unreachable. A real browser caught a second, different lie: the accounts walk spent its first hour debugging code that was never running, because two Docker engines each held a Hearth stack and a stale one owned the host ports, so every `500` the browser saw was one the running API never logged. The tell was in the response the whole time — a request-ID hostname that never matched the container, a per-process counter that never reset across a restart — unread because nobody had checked which process was actually answering. |
| **`finding-disclosure-oracles`** | Building or reviewing sign-in, password reset, magic links, invites, member lookup — anything whose contract is "answer the same way regardless". | Four leaks here were built from timing, side effects and error asymmetry, on endpoints returning byte-identical responses. One was readable by watching *which deadline moved*. |
| **`guarding-partial-writes`** | Writing or reviewing any create/update flow, any handler with a request struct, any repository method, any code where two things must both happen. | Four defects returned success for work that only partly happened. One left an orphaned row holding a unique index, making an invite **permanently unacceptable** with no recovery. |
| **`maintaining-system-design`** | A feature ships, a route or its guards change, a table or port changes, a flow is reshaped, or anything is refactored across a boundary. | `docs/SYSTEM_DESIGN.md` is what a human engineer reads to onboard. Its value is entirely in being accurate — a diagram that is 80% right is worse than none, because it is believed. |

---

## How they fit together

They cluster by when you reach for them:

**Steering** — `hearth-product-driver`. Before work starts, and after it ships.

**While writing** — `guarding-partial-writes` is the one to hold in mind at the
keyboard: it is about the shape of the code, not about checking it afterwards.

**While reviewing a diff** — `hunting-sibling-defects`,
`finding-disclosure-oracles`, `guarding-partial-writes`. Three different
questions to ask of the same change: where else does this appear, what can a
caller measure, and what survives a failure halfway through.

**Before claiming done** — `proving-tests-can-fail` and
`verifying-in-the-real-environment`. Both answer the same underlying question:
is the evidence real, or does it only look like evidence?

**As part of shipping** — `maintaining-system-design`, alongside the tracker and
learning-log updates. Documentation written afterwards is documentation written
never.

---

## The agents

| Agent | Use it when | Why it exists |
|---|---|---|
| **`hearth-architect`** | Before building something whose shape is not obvious: "how should we build X", "do we need a new service/table/port/library", "which of these two approaches", or when a change is about to cross an architectural boundary. Read-only — it returns a design, never a diff. | The rules in `CLAUDE.md` are strict enough that a generic architect contradicts them, and the expensive mistakes here are structural, not typographical. It refuses by default: no new component unless someone can name what breaks without it, and a port with one implementation and no second caller is the wrong shape. Its answer always ends with what it *deliberately did not build*, which is the record that stops the next person adding it anyway. |

An agent is worth writing when the job needs a fresh context and its output
should be reviewed rather than trusted. If the job is "remember to do this while
I work", that is a skill instead.

---

## What these deliberately do not cover

The installed skill sets already handle the general workflow, and duplicating
them would only create competing instructions:

- Designing a feature before building it → `superpowers:brainstorming`
- Turning a spec into a plan → `superpowers:writing-plans`
- Executing a plan with reviews between tasks → `superpowers:subagent-driven-development`
- Test-first development → `superpowers:test-driven-development` or `mattpocock-skills:tdd`
- Debugging something broken → `superpowers:systematic-debugging`
- Reviewing a branch → `mattpocock-skills:code-review`
- Not claiming success without evidence → `superpowers:verification-before-completion`

The seven above are the gaps those leave: the specific mistakes this codebase
actually made, and the roadmap nobody owned.

## Adding a skill

Write it when a defect class repeats, or when a job keeps being forgotten. One
skill per idea, with a description that says both what it does and when to reach
for it — that description is the whole triggering mechanism.

Ground it in what actually happened. Every skill here cites the real defect it
came from, because a rule with evidence behind it gets followed and an abstract
one gets skipped.

Then add a row above.
