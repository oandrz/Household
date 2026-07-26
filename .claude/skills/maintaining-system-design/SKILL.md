---
name: maintaining-system-design
description: Keep docs/SYSTEM_DESIGN.md true — the component, flow and data diagrams a human engineer uses to onboard. Use whenever a feature ships, a route or endpoint changes, a table or column is added, a port or interface changes, a service or adapter appears, a request flow is reshaped, or anything is refactored across a boundary. Update it in the same change as the code, because a diagram nobody trusts is worse than no diagram at all.
---

# Maintaining the system design document

`docs/SYSTEM_DESIGN.md` is what a new engineer reads to understand the shape of
the system before touching it. Its value is entirely in being accurate. A diagram
that is 80% right is worse than none, because it is believed.

The failure mode is not malice or laziness — it is that the code change felt
finished when the tests went green, and the diagram was a separate task that
never got scheduled. So the rule is simple: **update it in the same change**,
not afterwards.

## When this triggers

Anything that changes the shape a reader would draw:

| Change | What to revisit |
|---|---|
| A feature ships | Whichever flow it belongs to; add a sequence diagram if the flow is new and non-obvious |
| A route added, removed, or its guards changed | §4 route table and the pipeline diagram |
| A table, column or relationship | §6 ER diagram, and the notes under it if a constraint or nullability carries meaning |
| A port or interface changed | §3 ports table, and §2 if a new adapter appeared |
| A new service, adapter, or container | §1 or §2 diagram |
| A request flow reshaped | §4 pipeline, and any sequence diagram it touches |
| A refactor across a boundary | §2 — that is exactly what the layer diagram is for |
| Middleware order changed | §4 — the order *is* the security model, so say so |

If a change touches none of these, it does not belong in this document. Not
every commit needs a diagram edit, and padding it makes it harder to read.

## How to update it

**1. Read the section before editing it.** The diagrams are deliberately plain —
no colours, no styling — so they stay readable in any renderer and diff cleanly.
Keep that.

**2. Change the diagram *and* the prose under it.** Most sections carry a few
sentences saying what is not obvious from the shapes: why membership is re-read
on every request, why the invite is claimed first, why the reads in the
magic-link flow always both run. Those sentences are where the value is. A
diagram edit that leaves stale prose beneath it has made the document *less*
true, not more.

**3. Verify against the code, not against memory.** Read the router, the
migration, `ports.go`. This document was built that way and the details are
load-bearing — guard order, nullability, which reads run unconditionally.

**4. Say what is not built.** The document opens by scoping itself to what
exists. When a slice lands, update that line. When you add a diagram for
something partially built, mark the unbuilt part rather than drawing an intention
as though it were real.

**5. Check the Mermaid renders.** A broken diagram block is invisible until
someone opens the file. Node text containing `(`, `[`, `{`, `:` or `,` usually
needs quoting: `A["GET /auth/me — cached as ['me']"]`.

## The bar

Ask: **could someone who has never seen this repository read the document and
then find their way around the code?** If a name in the diagram does not appear
in the codebase, or a flow skips a step that exists, the answer is no.

Two specific things worth protecting, because both were expensive lessons here:

- **Authorisation lives only in the HTTP middleware.** No service takes an actor.
  If a diagram ever suggests a service checks who is asking, that is wrong and
  worth fixing immediately — someone will build on the misunderstanding.
- **Deliberate decisions read like defects unless explained.** The
  always-202 magic link, the household-wide lockout, the fire-and-forget send.
  Where a flow looks odd, the prose says why. Keep those sentences when you edit
  around them.

## Related documents

Do not duplicate them — link instead.

- `docs/FEATURE_TRACKER.md` — what exists and what does not
- `docs/HANDOVER.md` — current state and what to build next
- `docs/LEARNING.md` — defects and their patterns
- `CLAUDE.md` — the architecture rules this document illustrates
