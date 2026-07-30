# Self-serve sign-up — verification walkthrough

Run 2026-07-30, from a wiped database (`make down`, `docker volume rm
hearth_hearth-pgdata`, `make up && make seed`), in a real Chrome via browser
automation, against http://localhost:5173 with Mailpit at
http://localhost:8025. Walked by an agent, criteria from Task 32 of
`2026-07-27-hearth-self-serve-signup.md`.

This walk ran three days after the slice's code was finished and reviewed —
the grouped Money sidebar and the finance-fixes branch had landed in between,
which changes how criterion 7 reads (noted there). Nothing else it exercises
was touched by that work.

**Result: 15 of 15 pass.** No product defect found. Two criteria were met by
an interpreted rather than fully literal path, and one needed the API
restarted first to un-count the walk's own earlier requests; each is recorded
at the criterion rather than passed over quietly.

| # | Criterion | Result |
|---|---|---|
| 1 | Sign-in footer reads "No household yet?" with a "Create one" link | PASS |
| 2 | `/sign-up` shows only an Email field — no "Forgot?", no "or" divider, no magic-link button | PASS |
| 3 | Submitting `founder@example.test` renders the "Check your email." panel, copy covering both outcomes | PASS |
| 4 | The sign-up mail arrives and its link opens the details form | PASS — mail read via the Mailpit API rather than its UI; the link opened in the same real browser |
| 5 | Form shows the email read-only, the household-name helper, an unchosen currency select, "Your name", and the "At least 12 characters" hint | PASS — read-only proven by typing at the focused email field and watching it refuse the input |
| 6 | An 8-character password is refused without consuming the link | PASS — "Password must be at least 12 characters." inline; the same link then succeeded (step 7), which is the proof it was not consumed |
| 7 | Proper submit with BRL lands signed in, sidebar showing Overview, Money, Marriage, Family, Settings | PASS — Money renders in the design's grouped form (uppercase MONEY label with Finances and Transactions links), built by the finance-fixes branch after this plan was written; Settings and Sign out sit in the sidebar footer |
| 8 | Settings shows BRL primary, secondary toggle off, no IDR mention | PASS — the secondary row reads "Show BRL equivalents", i.e. the provisioning default of secondary = primary, and IDR appears nowhere |
| 9 | Re-opening the used link explains it was already used and offers Sign in | PASS — "That link won't work. This link has already been used." |
| 10 | Re-submitting the now-registered address renders the identical panel; Mailpit gets a "you already have an account" mail with no token link | PASS — subject "You already have a Hearth account", its only link the plain `/sign-in` URL |
| 11 | Six rapid fresh-address sign-ups: first five render "Check your email." identically, the sixth errors, Mailpit holds five mails not six | PASS — sixth showed "Too many requests. Try again later."; walk1–walk5 got one mail each, walk6 none. The API was restarted first: the limiter is per-process and in-memory (`middleware_ratelimit.go` says so), and the walk's own criteria 3 and 10 had already spent two of the five requests. In development Vite proxies `/api` with no header rewriting, so this exercises the limiter as `chi` resolves IPs there — the `X-Real-IP`/`True-Client-IP` hardening lives in the production nginx config and stays unexercised by this walk, as `docs/HANDOVER.md` §5 already records |
| 12 | Invite a second member as owner; accept in a private window; two owners; invite copy names the household | PASS — accepted after signing out in the same browser rather than in a private window, the same fresh-visitor state; invite screen read "Casa Founder / Fabio Founder invited you in / Joining as co-owner"; Members then listed both with Owner badges |
| 13 | Andreas's household untouched: SGD primary, IDR secondary, toggle on, four members | PASS — with the criterion's "four members" read as the seeded state: three accepted members (Andreas, Kayla, Ethan) plus Christine's deliberately pending invite, which the members list does not show until accepted. Same wording-versus-seed mismatch class as the Accounts walk's criterion 12; fixed here in the record, not the product |
| 14 | `adminctl unlock-household --email founder@example.test` succeeds against the new household | PASS — "Household unlocked."; the command resolves the household through the member email, and `founder@example.test` exists only in Casa Founder, so it cannot have touched Andreas's |
| 15 | `adminctl prune --older-than 3` refuses, naming the seven-day floor | PASS — "adminctl: --older-than must be at least 7 days: pruning login attempts inside the lockout window would clear a live lockout", exit 1 |

## Gate

```
make lint   — architecture lint, tsc, eslint, go vet: all clean
make test   — Go suite all ok (8 packages with tests, no failures);
              frontend: Test Files 23 passed (23), Tests 191 passed (191)
```

Run on the same tree the walk used, against Docker Desktop's engine (colima
was not running; `/var/run/docker.sock` symlinks to Docker Desktop's socket,
so testcontainers needed no `DOCKER_HOST` override).

## What the walk changed

Nothing in the product. The two interpreted paths and the limiter restart are
recorded above; the sign-up flow itself behaved exactly as specified,
including the enumeration-safe properties (identical panel and identical
silence for fresh, registered and rate-limited addresses — the only observable
difference is the per-IP 429, which reveals nothing about any address).
