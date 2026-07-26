# Hearth — user guide

A shared dashboard for a household. Today it handles who is in the household
and what each person can see. Money, Marriage and Family are placeholders for
now — the navigation is real, the pages behind most of it are not built yet.

---

## Starting it up

```bash
make dev      # starts everything, keeps the logs on screen
make seed     # creates the household and prints your sign-in details
```

Then open **http://localhost:5173**.

`make seed` prints something like:

```
Seeded the household "Andreas & Christine".
  Andreas:            andreas@hearth.family / hearth-dev-password
  Christine's invite: http://localhost:5173/invite/hearth-dev-invite-token
```

Stop everything with `make down`. Your data survives; the database lives in a
Docker volume. To start genuinely fresh, remove that volume:

```bash
make down
docker volume rm hearth_hearth-pgdata
make up && make seed
```

---

## Signing in

Use the email and password `make seed` printed.

**If you would rather not type a password**, choose *Email me a one-time
sign-in link*. In development all mail is caught by **Mailpit** at
**http://localhost:8025** — open it, find the message, click the link. The link
lasts 15 minutes and works once. You can request three per hour.

**Three wrong passwords locks the household for 15 minutes.** Not just that
account — everyone's password sign-in. The screen counts down as you go: "Two
tries left", then "One try left".

A magic link still works while the household is locked. That is deliberate — it
is the way back in. If you would rather not wait or use email:

```bash
make unlock-household
```

**Forgotten the password entirely:**

```bash
make reset-password EMAIL=andreas@hearth.family
```

It asks for the new password on screen without showing it, and signs out
everywhere that account was logged in.

---

## Adding people

Two kinds of people live in a household.

**Adults** get an email address, a password and full access. They are *owners* —
equals, with no hierarchy between them.

**Children** are members without accounts. They appear in the household, they
can be given or denied access to specific areas, but they cannot sign in. The
kids' view is not built yet.

### Inviting someone

Go to **Settings → Members → + Invite**. Give a name, choose a role, and pick
what they can access. For an adult, add their email; they get an invite link.
For a child, the email is optional — leave it out and they are simply added.

An invite lasts **7 days** and works once. The person opening it chooses their
own password, between 12 and 256 characters.

If you are already signed in when you open someone else's invite link, Hearth
warns you first — accepting it will sign you out and sign them in. That matters
on a shared laptop.

### What each person can access

Four switches, per person, in **Settings → Members**:

| | |
|---|---|
| **Calendar** | the shared family calendar |
| **Chores & allowance** | chores and pocket money |
| **Money & balances** | accounts, budget, bills — **off for children by default** |
| **Marriage** | the parents' private space — **never available to a child** |

Owners always hold all four. Turning one off for an owner is not possible;
demote them first if that is really what you want.

**The household must always keep at least one owner.** Removing or demoting the
last one is refused, with an explanation.

Changing someone's access signs them out of any other device, so the change
takes effect immediately rather than whenever they next reload.

---

## Spaces

The sidebar is built from *spaces* — groups of related pages.

| Space | Who sees it |
|---|---|
| **Money** | anyone given the Money access switch, adults or children |
| **Marriage** | parents only, always |
| **Family** | everyone in the household |

You can add your own in **Settings → Spaces → + New space**, choosing whether it
is visible to **Everyone** or **Parents only**. A third option, *Custom*, is
shown but not yet available — per-space membership is not built.

Each person only ever sees the spaces they are entitled to. A child with only
Calendar access sees Family and nothing else.

---

## Settings

**Members** — everyone in the household, their role, and their four access
switches. Children show their age. Email addresses are visible to owners only.

**Spaces** — the spaces in the sidebar and who each is for.

**Currency & region** — your primary currency, whether to show a second currency
alongside it, and how the exchange rate is obtained.

**Notifications** — four reminders: bills due, overspending, the monthly
check-in, and the weekly summary.

Only owners can change any of these. Everyone can see them.

---

## What is not built yet

The sidebar shows where these will go. Clicking through tells you which stage
each belongs to.

- **Money** — accounts, transactions, budget, savings goals, bills
- **Marriage** — monthly check-ins, vision and goals, shared agreements
- **Family** — the shared calendar
- **Overview** — the home screen pulling the above together
- **Kids' view** — a limited view for children
- **Connected accounts** — linking a bank. Note that automatic bank sync is not
  possible for an app like this; accounts will be added manually or by
  importing a file.

---

## When something goes wrong

**Locked out.** Use a magic link — it works while locked. Or `make
unlock-household`.

**No email arrived.** In development, mail never leaves your machine: check
Mailpit at http://localhost:8025. If it is not there, the link genuinely was not
sent — request another. Hearth deliberately never tells you whether an address
belongs to a member, so a request for an unknown address looks exactly the same
as one for a real one.

**Signed out unexpectedly.** Someone changed your access, or an invite was
accepted on this browser. Sign in again.

**"That name is already taken"** when adding a space — space names must be
unique within a household.

**An invite will not send.** If the address already belongs to a member, Hearth
refuses rather than sending a link that could never be accepted.

**Nothing loads at all.** Check the stack is up with `make ps`, and the logs with
`make logs`.
