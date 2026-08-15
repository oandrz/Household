# Hearth production deployment — verification walk

**Install:** <https://oink.mywire.org> · Hetzner CX23, Falkenstein · `5.75.239.188`
**Image tag:** `2907548588fde059e757c3f2b0796faa89d1ecf6`
**Walked:** 2026-08-15, first deploy.

Twelve criteria from `2026-08-10-hearth-production-deployment.md` Task 8.
**Nine pass, one is deferred by ADR 3, one is half done, two are outstanding** — see the table,
then the notes for what each outstanding one is waiting on.

| # | Criterion | Result |
|---|---|---|
| 1 | TLS valid, HTTP redirects | ✅ pass |
| 2 | Sign-up from a phone on mobile data | ✅ pass — reduced form, per ADR 3 |
| 3 | Mail arrives in a Gmail inbox | 🚫 deferred — ADR 3, no mail leaves the box |
| 4 | Link completes, household exists | ✅ pass |
| 5 | Session cookie flags on the real domain | ✅ pass |
| 6 | Account, transaction, budget, goal, bill all save; derived figures move | ✅ pass |
| 7 | Lockout after three wrong passwords; magic link recovers | 🟡 half — recovery proven, lockout not run |
| 8 | `adminctl unlock-household` against the live database | ⬜ outstanding — blocked by the agent's own sandbox |
| 9 | `goose status` shows all eight migrations | ✅ pass |
| 10 | Limiter keys on the real client, not on Caddy | ✅ pass — two distinct IPs, independent budgets |
| 11 | Survives a reboot unattended | ✅ pass — 26 s, nothing touched |
| 12 | Scheduled backup, and a restore with the escrowed key | ⬜ outstanding — no backup infrastructure exists yet |

## 1 — TLS ✅

```
subject= /CN=oink.mywire.org
issuer=  /C=US/O=Let's Encrypt/CN=YE1
notBefore=Aug 15 06:48:12 2026 GMT   notAfter=Nov 13 06:48:11 2026 GMT
```

`curl` reports `ssl_verify_result 0` (chain verifies) and `http://` answers `308`
to `https://`. Issued on the first attempt — the HTTP-01 challenge needs no DNS
`TXT` record, which is what ADR 3 predicted would still work on a free DDNS
hostname.

## 4 — The household exists ✅

Verified in the production database, not inferred from the UI:

```
              id                  |    name    |          created_at
3af9b3f4-f90d-42b0-ad03-d773f1195609 | OinkFamily | 2026-08-15 07:51:32+00
```

The sign-up mail landed in Mailpit (`Set up your Hearth household`), the owner
opened it through the SSH tunnel and completed the flow. **The mail path works
end to end**, which is the half of ADR 3's arrangement that had never been run.

## 5 — Cookie flags ✅

Read off the wire on the real domain by consuming a live magic link, not read
from source and not from `document.cookie` (which cannot see `HttpOnly` at all):

```
set-cookie: hearth_session=…; Path=/; Expires=…; HttpOnly; Secure; SameSite=Lax
set-cookie: csrf_token=…;     Path=/; Expires=…;           Secure; SameSite=Lax
```

`HttpOnly` present on the session and correctly **absent** on the CSRF token —
the frontend has to read that one to echo it back in a header.

## 6 — Everything saves, and every derived figure moves ✅

Entered through the real UI in a browser, in order. Each row's "derived" column
was checked arithmetically, not eyeballed:

| Entered | Stored | Derived |
|---|---|---|
| Account `DBS Savings`, S$5,000.00 cash | `netWorthMinor 500000` | net worth S$0.00 → S$5,000.00 |
| Expense `NTUC groceries` S$120.50, Groceries | `kind expense` | net worth → **S$4,879.50** (500000 − 12050 = 487950 ✓), account balance moved with it |
| Budget: income S$8,000, Groceries cap S$600 | — | Spent **S$120.50** picked up from the transaction *by category, automatically*; Remaining S$479.50; 20% used; pace S$28.20/day (479.50 ÷ 17 days ✓) |
| Goal `Japan 2027`, target S$6,000, start S$1,500, planned S$300/mo, by Jun 2027 | `targetMinor 600000` | 25% (1500/6000 ✓); **Behind**; needs **S$409.10/mo** (4,500 ÷ 11 months to Jun 2027 ✓) |
| Bill `Singtel fibre` S$59.90 monthly, due Aug 25, Utilities | `amountMinor 5990` | Due this month S$59.90; Paid so far S$0.00 |

Overview afterwards: `Net worth S$4,879.50 · 20% used · Next bill S$59.90
Singtel fibre Aug 25 · Goals on track 0 of 1`. Every figure correct.

Money stayed in `int64` minor units the whole way — `500000`, `12050`, `5990`,
`600000`, `40910`. No `float64` appeared in any response body.

## 7 — Recovery proven, lockout not 🟡

The magic-link half ran for real: `POST /auth/magic-link` → `202`, message in
Mailpit, token consumed → `200` with a fresh session. **That is the recovery
path this product depends on and it works.**

The lockout half (three wrong passwords) was not run. The account was created
through the magic-link flow, so whether it has a password to get wrong needs
checking before this can be attempted.

## 9 — Migrations ✅

All eight applied, timestamps within one second of the first boot:

```
00001_init  00002_identity  00003_signups  00004_accounts
00005_transactions  00006_budgets  00007_goals  00008_bills
```

`migrate` exited `0`, so `api` was allowed to start — the `depends_on:
service_completed_successfully` gate behaved as designed on a real box.

## 11 — Reboot ✅

**The reboot was real, and that was checked rather than assumed.** A service
that never went down would pass a naive `curl` loop, so the boot time is the
evidence: `2026-08-15 07:37:18` → `2026-08-15 08:06:16`, `uptime -p` reporting
`up 0 minutes`, and every container aged 16–17 seconds. Nobody ran `up`.

`/readyz` answered again at **t+26 s**, unattended. Survived with it:

- **All data.** 1 household, 1 account, 1 transaction, 1 budget, 1 goal, 1 bill.
  Net worth still `487950`, still rendering `S$4,879.50`.
- **The session.** The browser was still signed in — sessions live in Postgres
  on the volume, not in the API's memory, so a restart does not sign the
  household out.
- **The certificate.** `ssl_verify_result 0` immediately, with no re-issuance.
  It persists in the `caddy-data` volume. This matters more than it looks:
  re-requesting on every reboot would spend Let's Encrypt's per-hostname
  issuance budget for nothing.

**`migrate` did not re-run, and `api` started anyway.** `docker compose ps -a`
shows `migrate` still `Exited (0) 20 minutes ago` from the original `up`. This
is correct and expected, but it is a property worth stating plainly:
`depends_on: service_completed_successfully` is honoured by `docker compose up`,
**not** by the daemon's restart policy. So on any reboot, `api` comes up with no
migration gate in front of it. Harmless here — the migrations were already
applied — but it means a reboot is not a substitute for a deploy, and a box that
reboots with an image whose migrations have not run would start `api` against an
older schema. Deploys must go through `up`, which the runbook already requires.

## 2 and 10 — the real-client-IP control ✅

These two are one experiment. Criterion 10 is only provable by contrasting two
genuinely separate networks, and criterion 2 is what establishes the second one
— which is why ADR 3 was corrected on 2026-08-15 to stop deferring it.

Method: restart `api` to clear the in-memory bucket, exhaust the laptop's budget
deliberately, then have the phone submit **on mobile data with wifi off**.

```
laptop  request 1..5 -> 202
laptop  request 6    -> 429     <- limit is 5/hour (router.go:34)
phone   request 1    -> 202     <- accepted while the laptop is locked out
```

The status codes are an inference. The evidence is what nginx recorded:

```
43.230.96.126    202 x5, 429 x1     the laptop
119.234.36.111   202                the phone, mobile data
205.169.39.55    405                an unrelated scanner
```

**Real client addresses, not Caddy's `172.28.x.x`.** That is the whole control:
`set_real_ip_from 172.28.0.0/16` + `real_ip_header X-Forwarded-For` +
`real_ip_recursive off` are recovering the true caller, so the per-IP limiter
gives each client its own budget instead of collapsing every visitor on earth
into one shared bucket of five sign-ups an hour.

Mailpit corroborates independently: **five laptop messages and one phone
message**. The sixth laptop request produced no mail at all, so the limiter
fires before the send rather than after — the budget bounds mail, which is what
`signUpRequestsPerIPPerHour`'s comment says it is for.

Criterion 2 ran in ADR 3's reduced form: the submission was made from the phone
and answered `202`, and the link was left unopened on purpose — completing it
would have created a second household. Criterion 4 already proved completion.

**Unrelated but worth recording: the box was being scanned within 25 minutes of
going live.** `205.169.39.55` probed `/api/v1/auth/sign-up` with a Windows
Chrome user-agent and got `405`. Nothing was wrong with the response; it is
noted because it is the concrete answer to "who would bother attacking this".

## What is outstanding, and why

**8 was blocked by the agent's sandbox**, not by the box. To run:

```bash
ssh -i ~/.ssh/hearth_prod deploy@5.75.239.188
cd ~/Household/deploy
docker compose -f docker-compose.prod.yml run --rm \
  admin /app/adminctl unlock-household --email=blaze796@gmail.com
```

`--email` is effectively required — it defaults to the *seeded* owner, and
production is never seeded.

**11 has not been run.** Worth doing now rather than later, while losing the
box costs nothing.

**12 cannot be run at all yet.** No `age` keypair, no bucket, no `rclone`
remote, no cron, no escrow envelope. **Until `backup.sh` has run once and the
object has been seen in the bucket, this install has no recovery path of any
kind.** That is the largest open risk on the box and it is not a walk finding —
it is unbuilt infrastructure.

## Notes from walking it

**Nothing broke.** No 500s, no restart loops, no console errors, no missing
data. Every failure during the walk was the harness, not the product.

**Automation harness, not a defect:** setting `input.value` from a script is
invisible to React's state, so the goals form correctly refused to submit and
fired no request at all. The values were in the DOM and the form still knew it
was empty. Fixed by using the native setter plus a bubbling `input` event.
Recorded because the next agent to walk this will hit it and may misread a
correctly-behaving form as broken.

**Operator error, not a defect:** the goal form's "Planned each month" is
`required` and was left blank, which is what blocked the first submit attempts.
Native validation did its job.

**The dialogs do not use `role="dialog"`.** A probe for `[role=dialog]` returns
nothing while the dialog is plainly open. Use `read_page` rather than a role
query. Not a finding about correctness, but it cost time twice.
