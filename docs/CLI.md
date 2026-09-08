# `hearthctl` — driving Hearth from a terminal or an agent

`hearthctl` is a command-line client for the Hearth API. It exists so a
script, a cron job or an AI agent can put data into the product (and read it
back) without a browser. It talks to the same routes the web app uses, with
the same guards, so nothing it can do is something the signed-in member
could not do by clicking.

## Build

```bash
cd api && go build -o ../bin/hearthctl ./cmd/hearthctl     # or: make hearthctl
```

## Sign in once

```bash
export HEARTH_URL=http://localhost:8080        # the Go API, not Vite's 5173
hearthctl login --email=andreas@example.com    # prompts for the password
# non-interactive:
HEARTH_PASSWORD='…' hearthctl login --email=andreas@example.com
```

The session lasts 30 days and is stored in `~/.config/hearth/<host>.json`
(mode 0600, one file per host so a dev session never mixes with production).
Set `HEARTH_CONFIG_DIR` to put it somewhere else. `hearthctl logout` revokes
it. `hearthctl whoami` prints who you are — including your **membership id**,
which the transaction and bill inserts take as `--paid-by`.

**Do not loop on login.** Five wrong passwords lock the whole household out.
The CLI itself never retries.

## Insert data

Amounts are **minor units**, exactly as on the wire: `--amount-minor=1234`
is 12.34 in the account's currency. Dates are `YYYY-MM-DD`, months
`YYYY-MM`. Ids come from `hearthctl list …`.

```bash
hearthctl list accounts        # → account ids
hearthctl list categories      # → category ids
hearthctl list members         # → membership ids

hearthctl transaction add --kind=expense --date=2026-09-08 \
  --description="Groceries" --amount-minor=8450 \
  --from-account=<account-id> --category=<category-id> --paid-by=<membership-id>

hearthctl transaction add --kind=income   --date=2026-09-01 --description="Salary" \
  --amount-minor=650000 --to-account=<account-id>
hearthctl transaction add --kind=transfer --date=2026-09-02 --description="To savings" \
  --amount-minor=100000 --from-account=<id> --to-account=<id>

hearthctl account add --nickname="DBS Savings" --type=cash --currency=SGD \
  --as-of=2026-01-01 --opening-balance-minor=500000
hearthctl bill add --name=Rent --amount-minor=250000 --cadence=monthly \
  --next-due=2026-10-01 --category=<id> --pay-from-account=<id> --paid-by=<id> --autopay
hearthctl goal add --name="Japan trip" --target-minor=800000 --currency=SGD --target-month=2027-03
hearthctl category add --name=Groceries
```

Every command prints `-h` for its flags.

## Reach anything else

`hearthctl routes` prints every route, who may call it, and its body shape.
`hearthctl api` calls one:

```bash
hearthctl api GET  '/transactions?month=2026-09&kind=expense'
hearthctl api PATCH /transactions/<id> --data='{"description":"Coffee beans"}'
hearthctl api DELETE /transactions/<id>
hearthctl api PUT /budgets/2026-09 --data=@budget.json
```

Paths are relative to `/api/v1`. The CLI adds the CSRF header on every
non-GET; you never handle it.

## Output and exit codes

The API's JSON goes to **stdout** untouched (pipe it to `jq`); anything for a
person goes to stderr. A 204 prints nothing.

| Exit | Meaning |
|---|---|
| 0 | success |
| 1 | usage error (bad flag, missing value, invalid JSON) |
| 2 | sign in again — the API answered 401, or there is no stored session. From `login` itself it means the sign-in was **refused** (wrong password or a locked household): the body on stdout carries `attemptsRemaining`, and the right move is to stop, not retry |
| 3 | the API refused (any other 4xx/5xx); its own error body is on stdout |
| 4 | could not reach the server |

## Limits to know

- **Writes need an owner.** A limited member's session gets 403 on every
  insert, the same as in the app.
- **No retries on writes.** The API has no idempotency keys, so a retried
  POST is a duplicate row. If a write's exit code is 4 you do not know
  whether it landed: `list` before trying again.
- **Money-capability routes are gated per member.** `routes` shows the guard
  for each.
- Sign-up, magic link, Telegram and invite acceptance are browser flows and
  are not wrapped; `api` can still reach them.

## For an agent

The shortest reliable loop is: `whoami` (checks the session, yields the
membership id) → `list accounts` and `list categories` (ids) → the insert →
read back with `list transactions` or `api GET`. Treat exit 2 as "stop and
ask the person to run `hearthctl login`", never as "try the password again".
