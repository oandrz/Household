# Hearth — skill tracker

Project skills live in `.claude/skills/<name>/SKILL.md`. Each one exists because
a real defect got through in this project, or because a job needed an owner.

Add a row here whenever you add a skill, and say plainly when to reach for it.

---

## The skills

| Skill | Use it when | Why it exists |
|---|---|---|
| **`hearth-product-driver`** | Anyone asks what to build next, whether something is already built, how much is left, or wants to start a new area. Also whenever a feature ships, to record it. | The roadmap needs an owner. Without one the feature tracker decays into fiction and the product gets built out of dependency order — Overview before Money means building Overview twice. |
| **`hunting-sibling-defects`** | Immediately after fixing any bug, before closing it. Also when a review finds a defect. | Fixing one instance failed to fix the class **five times** here. A PATCH corrected in two of three endpoints; an error oracle closed at the mailer and left two lines below; a non-awaited invalidation fixed in one panel with two siblings untouched. |
| **`proving-tests-can-fail`** | Writing a test for a guard, a rule, an ordering, or a security property. Also when a suite passes on a change you expected to break something. | Five tests here passed against deliberately broken code. A fixture that agreed with the wrong answer, a stub that ignored the URL, a test that waited past the window where the bug lived. |
| **`verifying-in-the-real-environment`** | Work touches the DOM, a browser API, SQL, a transaction, a migration, or wiring you introduced. Before claiming a UI feature works. | jsdom's `<dialog>` is a stub, so five green tests hid a modal that threw on **every open** in production — and fixing that exposed a second bug that had been unreachable. |
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

The six above are the gaps those leave: the specific mistakes this codebase
actually made, and the roadmap nobody owned.

## Adding a skill

Write it when a defect class repeats, or when a job keeps being forgotten. One
skill per idea, with a description that says both what it does and when to reach
for it — that description is the whole triggering mechanism.

Ground it in what actually happened. Every skill here cites the real defect it
came from, because a rule with evidence behind it gets followed and an abstract
one gets skipped.

Then add a row above.
