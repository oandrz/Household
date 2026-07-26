---
name: verifying-in-the-real-environment
description: Verify behaviour in the environment it actually runs in — a real browser, a real database, the real wiring — not in a simulator. Use whenever work touches the DOM, a browser API, SQL, a transaction, a migration, or anything where a stub stands in for a platform. Also use before claiming a UI feature works. In this project a simulated DOM hid a modal that threw on every single open in production while all five of its tests passed.
---

# Verifying in the real environment

Test doubles are worth having. They are also, by construction, a description of
what someone believed the real thing does. When that belief is wrong, the tests
agree with the belief and stay green.

Three defects here were invisible to a simulated environment and obvious in a
real one:

- **`Modal` threw `InvalidStateError` on every open in every real browser.** React
  renders `<dialog open>` before the effect calls `showModal()`, and the HTML
  spec throws if the attribute is already there. All five tests passed, because
  jsdom's `HTMLDialogElement` is an empty stub with no `showModal` at all — so
  only the fallback path ever ran. The tests were exercising code that never runs
  in production, while the code that does always threw.
- **Fixing that exposed a second bug** that had been unreachable: the dialog never
  stretched to the viewport, so there was no backdrop area to click. It could not
  be found until the first bug was gone.
- **A 401 redirect bounced every invitee off the invite screen.** The suite was
  green because the handler defaults to null and every test installed a stub
  instead of the real wiring. The one seam the change introduced was the only
  seam nothing exercised.

## The move

**Run it where it runs. Look at it. Then write down what you saw.**

Not "focus trapping should work because `<dialog>` provides it" — open the modal,
press Tab, see where focus goes.

### For browser behaviour

Drive a real browser against the running stack:

```bash
make dev          # then use a browser automation tool against localhost:5173
```

Assert on things a simulator cannot fake:

- `dialog.matches(':modal')` — genuinely in the top layer, not merely not throwing
- `document.activeElement` after opening and after closing
- `getBoundingClientRect()` when layout is the question
- The console — an exception inside a React effect will not fail a test, and
  will not always break the page visibly either

### For database behaviour

Use a real database, not an in-memory double, when the question is about SQL:

- Constraints, cascades, unique violations, check constraints
- Transactions and rollback — force the *second* statement to fail and assert the
  first was undone
- Migrations — apply, roll back, apply again on a wiped volume
- Concurrency — two callers racing the same row

The in-memory doubles here are deliberately dumb: they store what they are given.
That is right for testing a service's logic and useless for testing whether the
database will accept the write.

### For wiring

If a change introduces a seam — a handler, a provider, a callback — write at
least one test that installs the **real** thing rather than a stub of it. A stub
of your own seam tests your stub.

## Tell the difference in your report

Say which environment produced the evidence:

> Verified in Chromium: `matches(':modal')` true, focus moved to Close, Escape
> returned focus to the trigger, backdrop click closed it. Two open/close cycles,
> no console errors.

versus

> Tests pass in jsdom; `<dialog>` is stubbed there so this exercises the fallback
> path only.

The second sentence is not a failure — it is honest scoping, and it tells the
next person exactly where to look when something breaks in production.

## When a simulator is enough

Most logic does not touch a platform. Pure functions, domain rules, error
mapping, state machines — a fast simulated environment is the right tool and a
browser is a waste of a minute. The trigger for this skill is a **platform
dependency**: the DOM, a browser API, SQL semantics, transactions, real
concurrency, or wiring you introduced yourself.
