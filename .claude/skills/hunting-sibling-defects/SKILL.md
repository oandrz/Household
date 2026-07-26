---
name: hunting-sibling-defects
description: After fixing any bug, find the other places the same mistake was made. Use this immediately after applying a fix, when a code review finds a defect, when a test starts passing again, or whenever you are about to close an issue — before you say something is fixed. In this codebase, fixing one instance has failed to fix the class five separate times, so treat "I fixed it" as the trigger, not the finish line.
---

# Hunting sibling defects

A defect is rarely unique. Whoever wrote it was following a habit, a pattern from
a nearby file, or a mental model that was slightly wrong — and they almost
certainly applied it more than once.

This happened **five times** while building this project. Each fix was correct.
Each sibling shipped anyway:

- `PATCH` implemented as a full replace, fixed in two endpoints, missed in a
  third. Found two tasks later by someone building against it.
- An error path that leaked whether an email belonged to a member, closed at the
  mailer and left in place **two lines below** at two other calls.
- A non-awaited cache invalidation fixed in one panel, left in two siblings.
- A pending-guard added to one control and not its neighbour.
- One error mapped off a 500 in the same review round that left its twin
  unmapped and still returning 500.

The cost of looking is minutes. The cost of not looking is finding it in
production, or in a review three tasks later.

## The move

After the fix is in and the test is green, before you close anything:

**1. Name the mistake in one sentence, at the level of the pattern.**

Not "the household PATCH blanked omitted fields" but "a PATCH handler used value
fields, so omitted and empty were indistinguishable." The second sentence is
searchable. The first is not.

**2. Search for the shape, not the symptom.**

Grep for the mechanism you just changed. Useful angles:

- The construct — `json.NewDecoder`, `onSuccess`, `errors.Is`, a `switch` with no
  `default`, `disabled={`
- The type or field — every handler with a request struct, every mutation hook
- The call — everything that calls the function you just fixed, and everything
  that calls its siblings
- The neighbours — if the bug was in `MembersPanel`, open `CurrencyPanel` and
  `NotificationsPanel` and read the same lines

**3. Read the hits, do not skim them.** The sibling usually looks slightly
different. In this project the twin of a value-fields PATCH was in a different
package with different field names.

**4. Say what you found, including nothing.**

Report the search you ran and the result. "Grepped every `json.NewDecoder` —
all nine go through the bounded helper" is a useful sentence. Silence is not: the
next reviewer cannot tell whether you looked.

**5. If you find siblings, fix them in the same change.** They are the same
defect. Splitting them across changes means one gets forgotten, which is the
failure mode this exists to prevent.

## Where to look first, in this codebase

Places where a class of defect has already appeared more than once:

| If you fixed… | Also check… |
|---|---|
| An HTTP handler's request struct | the other handlers in `internal/adapter/http` — all three PATCH endpoints have been wrong at some point |
| An error return that only one kind of caller can reach | every other early return in the same function, and the same function's siblings in `usecase/` |
| A React mutation's `onSuccess` | every other mutation hook — three had the same non-awaited invalidation |
| A control's `disabled` state | its sibling controls in neighbouring panels |
| A `switch` on a string that came from the database or a request | every other such `switch`; two lacked a `default` and failed open |
| An error-to-status mapping | the mappings either side of it in the table |

## What good looks like

> Fixed `PATCH /household/members/:id` to use pointer fields. Grepped for other
> request structs with value fields — `/household` and `/notification-preferences`
> were already converted in an earlier round, so this was the last one. Also
> checked the three POST handlers; they legitimately require every field.

That is a closed loop. The reader knows the class is dealt with, not just the
instance.
