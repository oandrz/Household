---
name: hearth-cli
description: Drive Hearth from the shell with hearthctl — insert transactions, accounts, bills, goals and categories, read anything back, and import a CSV of transactions safely. Use whenever a task says to put data into Hearth, automate the product, script it, or check what a household holds without opening a browser. Also the reference for exit codes, what never to retry, and how to resolve the ids every insert needs.
---

# Driving Hearth with `hearthctl`

`hearthctl` is a plain HTTP client of the Hearth API. It can do exactly what
the signed-in member can do, no more. Manual: `docs/CLI.md`. Why it is shaped
this way: `docs/adr/0006-a-cli-as-the-automation-surface.md`.

## Setup, once

```bash
make hearthctl                      # builds bin/hearthctl
export HEARTH_URL=http://localhost:8080      # the Go API, never Vite's 5173
bin/hearthctl login --email=<member email>   # password: $HEARTH_PASSWORD or stdin
```

The session lasts 30 days in `~/.config/hearth/<host>.json`. Production is a
different host and therefore a different file: `--url=https://oink.mywire.org`.

**Headless (cron, a scheduled agent, no person to type a password):** use a
personal API token. A person mints it once from a password login
(`hearthctl token create --name=<purpose>`; shown once), then the machine
runs `HEARTH_TOKEN=… hearthctl login --token`. From then on every call sends
`Authorization: Bearer`. A token can do what its member can do and nothing
more; it cannot mint or revoke tokens, sign out, or reach `/admin`. If a
token stops working (exit 2), it expired or was revoked: ask the person for
a new one, never retry.

## The loop

Every task follows the same four steps. Do not skip the first or the last.

1. **`hearthctl whoami`** — proves the session is live and prints your
   `membership.id`, which `--paid-by` takes. Exit 2 here means stop and ask
   the person to run `login`.
2. **Resolve ids.** `list accounts`, `list categories`, `list members`.
   Inserts take ids; `transaction import` also accepts names.
3. **Insert.** One of the typed verbs, or `api <METHOD> <path> --data=…` for
   anything `routes --json` lists.
4. **Read back.** `list transactions`, or `api GET '/transactions?month=YYYY-MM'`,
   and confirm the row is there. A 2xx is not the same as having seen it.

## Exit codes are decisions

| Exit | It means | What you do |
|---|---|---|
| 0 | done | read back, then report |
| 1 | you used it wrong | fix the flags; `<verb> -h` prints them |
| 2 | no live session, or sign-in refused | **stop**; tell the person to run `hearthctl login`. Never retry a password: five failures lock the whole household |
| 3 | the API refused; its error JSON is on stdout | read the body; it says which field is wrong |
| 4 | server unreachable | for a write, **you do not know whether it landed** — `list` before trying again, or use an idempotency key (below) |

## Rules that are not optional

- **Amounts are minor units.** `--amount-minor=1234` is 12.34. Never a decimal.
- **Dates are `YYYY-MM-DD`, months `YYYY-MM`.**
- **Never loop on `login`.** Once, then stop.
- **Writes need an owner's session.** A limited member gets 403.
- **Do not retry a write blindly.** Give it a key and the retry is safe:
  `transaction add --key=<anything unique to this event>` or
  `api POST /transactions --idempotency-key=<key> --data=…`. The same key
  with the same body returns the original row (200, not 201); the same key
  with a different body is refused with 409. See `docs/CLI.md`.

## Importing many transactions

```bash
bin/hearthctl transaction import statement.csv --dry-run   # resolves, posts nothing
bin/hearthctl transaction import statement.csv
```

CSV header: `date,kind,description,amount_minor,from_account,to_account,category,paid_by,key,received_minor`.
Account, category and paid-by accept a name or an id. Every row is checked
and resolved before the first request. Each row gets an idempotency key —
the `key` column if present, otherwise a hash of the row's content plus an
ordinal among identical rows — so **running the same file twice creates
nothing new**. The summary on stdout says `created`, `replayed`, `failed`.

## Reporting

Say what you inserted with its id, what the read-back showed, and quote the
API's error body for anything that failed. Do not say "done" on a 2xx alone.
