---
name: proving-tests-can-fail
description: Prove a test actually catches the bug it claims to, by breaking the code on purpose and watching it go red. Use whenever you write a test for a guard, a rule, an ordering, or a security property; whenever you are about to claim a behaviour is covered; and whenever a suite passes on a change you expected to break something. A green test proves nothing until you have seen it fail — five tests in this project passed against deliberately broken code.
---

# Proving tests can fail

A passing test is evidence of nothing on its own. It might be asserting on a
fixture that agrees with the wrong answer, stubbing the thing it means to check,
or waiting for the state it was supposed to catch mid-flight.

Five real examples from this codebase, all of which passed happily:

- A sidebar ordering test supplied items **already in the right order**, so a
  component that re-sorted them would have passed identically — and re-sorting
  was exactly the bug the test existed to prevent.
- `TestUsersWithoutAPasswordCannotSignIn` passed with the guard deleted, because
  the fake hasher rejected an empty hash for its own unrelated reasons.
- Five of seven invite tests used a fetch stub that returned responses
  **positionally, ignoring the URL** — they would have passed while the component
  called a completely different endpoint.
- A modal's five tests all passed while the component threw on every open in a
  real browser, because the test environment's `<dialog>` is a stub and only the
  fallback path ever ran.
- A "disabled while pending" test waited for the refetch to land before its
  second click, stepping neatly around the window where the bug lived.

Each was written in good faith. The author had no way to know, because they never
watched it fail.

## The move

**Break the thing on purpose. Watch the test go red. Put it back.**

```
1. Make the smallest edit that reintroduces the defect —
   delete the guard, remove the await, flip the comparison.
2. Run only the tests that should now fail.
3. Confirm they fail, and read the failure message.
4. Restore the code. Confirm green again.
5. Confirm the working tree is clean.
```

Step 3 matters as much as step 2. **Read the message.** A test can fail for the
wrong reason — a nil dereference where you expected an assertion, or every test
in the file failing when you expected one. If the whole file goes red, your
mutation was too broad and you have learned nothing about the specific test.

## What to check

Prioritise tests where being wrong is expensive and being wrong is invisible:

- **Guards** — anything gating on a role, a capability, a pending state, a lock
- **Orderings** — where the fixture might agree with the wrong answer anyway
- **Negative assertions** — "does not appear", "is not called", "returns nothing"
- **Security properties** — indistinguishability, authorisation, revocation
- **Anything you wrote to close a specific bug** — that is the test most likely
  to be shaped by the bug rather than by the behaviour

## Reading a fixture with suspicion

Before mutating, ask whether the fixture could produce the right answer for the
wrong reason:

- Does the input order match the expected output order? Then an implementation
  that sorts and one that preserves are indistinguishable. Shuffle it.
- Does a stub answer regardless of what it was asked? Then the call target is
  unverified. Match on it, and fail loudly on anything unregistered.
- Does the assertion check a count where it means to check a value? "Two
  capabilities" passes for the wrong two.
- Does the test wait for a state before asserting? Then it is testing after the
  window, not inside it.

## Report it

Write down what you mutated and what happened. One or two lines:

> Deleted the `RequiredCapability` guard from `VisibleSpaces`; exactly two tests
> failed with "want 1 space, got 2". Restored, green again.

This tells the next reader the test is load-bearing. Without it they have to
take your word for it — and the five examples above are what taking someone's
word for it looks like.

## When not to bother

Mutation-checking a test for a pure function with obvious inputs and outputs is
usually wasted effort — if `Add(2, 3)` returns 5, the test is not lying to you.
Spend the effort on the tests guarding behaviour you cannot see by reading.
