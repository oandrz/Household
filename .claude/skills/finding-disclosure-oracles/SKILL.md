---
name: finding-disclosure-oracles
description: Find the ways an endpoint reveals something it was designed to hide — through timing, side effects, error asymmetry, or how much work it does — not just through its status code. Use when building or reviewing sign-in, password reset, magic links, invites, member lookup, or any endpoint whose contract is "answer the same way regardless". Four real leaks in this project were built entirely out of things nobody thought of as output.
---

# Finding disclosure oracles

An endpoint designed not to reveal whether an account exists usually gets the
obvious part right: same status, same body, same wording. Then it leaks anyway,
because a caller can observe more than what you chose to send them.

All four of these were live in this codebase, on endpoints returning identical
responses:

- **Which deadline moved.** The locked branch of sign-in skipped recording the
  attempt, so `lockedUntil` froze — while the unknown-address path kept advancing
  its own deadline. Guessing at a steady rate and watching which one moved told
  you whether the address was real.
- **The cost of hashing.** `Verify` ran on only one of four branches. Argon2's
  deliberate expense — tens to hundreds of milliseconds — separated "real member
  with a password" from every other case, by wall clock alone.
- **An error only one kind of caller can reach.** The mailer's failure propagated
  on the known-address branch only. A degraded relay turned a non-nil result into
  a discrete yes/no membership signal — cheaper to exploit than any timing
  attack, because it needs one request, not a distribution.
- **Doing less work for one case.** Rate-limited requests were *faster* than
  unknown ones, because the count query joins through users and a stranger can
  never reach the limit. The fastest response meant "real member".

## The move

For each branch through the endpoint, write down everything a caller could
measure. Then compare the columns.

| | unknown address | wrong password | locked | rate-limited |
|---|---|---|---|---|
| status and body | | | | |
| every field, including timestamps and counters | | | | |
| repository calls made | | | | |
| expensive work performed (hashing, network) | | | | |
| errors that can reach the caller | | | | |
| side effects (mail sent, row written) | | | | |
| behaviour on the *next* request | | | | |

Any row that differs is a channel. Then judge whether it is exploitable and
whether it matters — not every difference is worth closing, but every difference
should be a decision rather than an accident.

## The four questions that find these

**1. Does every branch do the same amount of work?** If one returns before the
expensive operation, timing separates it. The fix is usually a decoy: perform the
same work and discard the result. Apply it on *every* early-return path — a decoy
on two of three branches just narrows the signal.

**2. Can every branch produce the same errors?** An error reachable only after
the "does this user exist" check is a membership oracle whenever it can be
induced. Either make it reachable by all branches or by none — log it and return
the uniform answer.

**3. Does anything asymmetric persist between requests?** Counters, deadlines,
rate limits, rows written. The leak is often not in one response but in the
*difference between two* — which is exactly why watching a deadline across
several requests worked here.

**4. Does the differing work happen before the branch, or after?** Work done
before you know which case you are in is safe. Anything after is a candidate.
Reordering so the uniform work happens first is often the whole fix — that is how
the magic-link read counts were equalised.

## What is not a leak

Don't manufacture findings. Two things here look like leaks and are not:

- **Invite tokens.** The preview distinguishes unknown, expired and
  already-accepted — but the identifier is 256 bits of randomness, so there is no
  space to enumerate. Telling the holder of a valid link why it is dead is good
  product behaviour.
- **Accepted disclosures.** The household-wide lockout leaks which addresses share
  a household, deliberately, because the design's own copy says the household
  locks. That is recorded in the code with its reasoning. Re-raising it as a
  defect wastes everyone's time — read `docs/LEARNING.md` and the comment at the
  site before reporting.

## Writing the decision down

When a difference is left in place on purpose, say so at the point someone would
try to remove it: what leaks, who decided, and what would change the decision. An
undocumented accepted risk gets "fixed" by the next person, or widened by someone
who assumes it was already considered.
