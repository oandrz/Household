# Accounts — definition-of-done walk

Walked 2026-07-28 against `feat/accounts`, in a real browser (Playwright driving
Chromium) on a wiped database: `make down`, `docker volume rm
hearth_hearth-pgdata`, `make up && make seed`.

**Result: 15 of 15 criteria pass.**

Two things went wrong that were not defects in the feature, and both are
recorded below rather than tidied away — one cost most of the walk's elapsed
time, and the other is a defect in this plan's own script.

---

## Before the criteria: the two-Docker-engine trap

The first hour of this walk chased a phantom. Every accounts route answered
`500 INTERNAL`, the Finances page rendered "Couldn't load your accounts", and
**the API logged nothing at all** — no `unhandled error`, no `panic recovered`,
despite both of those being the only two code paths that can produce that
response body.

The cause: **this machine runs two Docker engines.** Colima holds the stack
`make up` built, migrated and seeded. Docker Desktop had a *separate* Hearth
stack up for five hours, and it owned host ports 5173 and 8025 and 8080. So the
browser and every `curl` reached the stale stack, while every `docker compose`
command (with `DOCKER_HOST` pointed at colima, as `docs/HANDOVER.md` instructs)
managed an invisible one. Reading colima's API logs while Docker Desktop's API
answered is why nothing ever appeared.

The tell was in the response all along: the chi request ID's hostname prefix
(`dac8b65ae80e`) never matched the running api container (`3c78bec1b4a9`), and
the per-process request counter never reset across a `docker compose restart`.
Neither is possible for a process that is actually being restarted.

Confirmed by stopping the api container and watching `/api/v1/currencies` on
port 8080 still answer `200`.

Resolved by stopping the Docker Desktop stack's four containers — not removing
them, and not touching their volumes — and restarting colima's `web` and `api`
so the published ports rebound.

**The wiped volume was colima's.** The Docker Desktop stack's data is intact.

This belongs in `docs/LEARNING.md`: *when a service returns an error it did not
log, stop debugging the code and confirm you are talking to the process you
think you are.* Every hypothesis about the code was wrong because the premise
was wrong.

---

## The criteria

| # | Criterion | Result |
|---|---|---|
| 1 | Money opens Finances, not a placeholder; `/money/…` siblings keep theirs | **PASS** |
| 2 | No accounts → one first-run panel, not three empty cards | **PASS** |
| 3 | `+ Add account` opens the modal; no source picker, no Singpass card, no connected-bank strip | **PASS** |
| 4 | `DBS Everyday`, cash, Andreas, `8240.55` SGD → list and net worth both read `S$8,240.55` | **PASS** |
| 5 | `Car loan`, loan, `14500` → net worth `−S$6,259.45` | **PASS** |
| 6 | Editing the loan to `-14500` is refused | **PASS** |
| 7 | `BCA Tahapan`, `85400000` IDR → renders `Rp 85,400,000`; net worth rises ≈ S$6,881 | **PASS** |
| 8 | A USD account → net worth still shown, exclusion line names USD | **PASS** |
| 9 | Count-toward-net-worth off → total drops, bar stays, footnote appears | **PASS** |
| 10 | Archive removes from list, total and breakdown; Show archived + restore returns it | **PASS** |
| 11 | Primary currency → EUR: **no figure at all**, currencies named; back to SGD restores it | **PASS** |
| 12 | A limited member with `money` sees only shared accounts, no amounts, no summary | **PASS** |
| 13 | That member's direct `POST /api/v1/accounts` → `403` | **PASS** |
| 14 | Removing a member leaves their accounts in place as Shared | **PASS** |
| 15 | Money switched off → `GET /api/v1/accounts` → `403` | **PASS** |

### What the interesting ones actually showed

**Criterion 5 — the sign rule, end to end.** Net worth read `−S$6,259.45`; the
breakdown drew Loan as `−S$14,500.00`; and the accounts list showed the same
loan as `S$14,500.00`, **positive**. That is the design working exactly as
specified: the amount is stored as the sum owed, and the minus sign is derived
from the type at the point it means something. Nobody types a negative number.

**Criterion 6 — refused in the field.** "Enter what you owe as a positive
amount. Hearth adds the minus sign for a loan or credit card." shown in a
`role="alert"`, with the dialog still open and the typed value still there to
correct.

**Criterion 7 — convert-then-add, in production.** Net worth went from
`−S$6,259.45` to `S$622.10`, a rise of `S$6,881.55` — matching
`fx.StaticProvider`'s `{1, 12410}` rate applied to 8,540,000,000 IDR minor units
to the cent. The Cash & savings bar became `S$15,122.10`, which is
`8,240.55 + 6,881.55`. This is the rule that fails only once a second currency
exists, verified against a real database rather than a double.

**Criterion 9 — bars that deliberately do not sum.** Net worth dropped to
`−S$7,618.45` while Cash & savings stayed `S$15,122.10`, and both exclusion
lines appeared separately ("no exchange rate for USD" / "set not to count
toward net worth") along with the footnote "Includes accounts that don't count
toward net worth." The chart disagreeing with the total is intended, and the
screen says why.

**Criterion 11 — the state a Settings click reaches.** With the primary currency
on EUR: **no net worth figure rendered at all**, the copy read "We can't work
out a total yet: there's no exchange rate between EUR and SGD, IDR, USD", and
the Assets & liabilities card was absent entirely. No zero anywhere. Changing
back to SGD restored `−S$7,618.45` exactly.

**Criterion 12 — the redaction, at the wire.** The limited member's response
body carried one account (the only one with `visibleToLimitedMembers: true`),
and **no `balance` key, no `balanceAsOf` key, and no `summary` key**. Absent,
not zeroed. The three accounts not shared with them were not in the payload at
all.

**Criterion 14 — `ON DELETE SET NULL`, live.** `DBS Everyday` was reassigned to
Kayla, Kayla was removed (`204`), and the account came back with
`ownerMembershipId: null` and `ownerName: null` — present, and Shared. No
application code ran to make that happen.

**Criterion 3 — the modal.** It opened as a genuine `<dialog>` with its own
accessibility tree, both toggles at their specified defaults (count-toward-net-
worth on, visible-to-kids off), the Owner select listing every household member
plus Shared, and the currency defaulting to the household's primary rather than
to the alphabetically-first ISO code. This is the criterion that exists because
jsdom stubs `<dialog>` and five passing tests once hid a modal that threw on
every open.

---

## A defect in this walk's own script

**Criterion 12 as written says "sign in as Kayla in a private window". That is
not executable.** Seeded children are credential-less by design — no email, no
password — which is the whole point of how `ensureChild` creates them. There is
no way to sign in as Kayla.

The criterion was met instead by inviting a limited member who *does* have
credentials (`adminctl create-invite`), accepting the invite, granting them
`money`, and signing in as them. That is also the more realistic path: a
household that wants a teenager to see the accounts screen has to invite them
properly first.

Fix the script rather than the product.

---

## One observation, outside this branch

Accepting an invite through `POST /api/v1/invites/{token}/accept` with only a
password produced a membership whose display name is **empty**, even though the
invite carried the name "Teen Two". The member appears in the list with a blank
name.

This is slice 1 code, untouched by this branch, and it may simply be that the
endpoint expects a display name in the body that I did not send — in which case
the defect is that it accepted the request and created a nameless member rather
than refusing. Either way it is not this feature's, and it is recorded here so
it is not lost.

---

## The gate

`make lint` and `make test` were run green on this tree before the walk began
and are recorded in the task reports. The walk added no code.
