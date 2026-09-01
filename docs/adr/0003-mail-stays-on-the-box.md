# 3. Mail stays on the box until a real domain exists

**Status:** Accepted — 2026-08-12. Explicitly interim; see "Exit condition".
**Amended 2026-09-01** — the exit condition changed, because
[ADR 4](0004-telegram-as-a-second-delivery-channel.md) added a delivery channel
that carries strangers without mail. The amendment is at the bottom, and the
original text is left standing above it.

## Context

The first production install runs on `oink.mywire.org`, a free Dynu DDNS
hostname. Getting a hosted relay to send from it turned out to be impossible,
for a reason that is worth writing down because it is not obvious:

Dynu refuses `TXT` records on free, newly created third-level hostnames — *"This
DNS record type is only available for members, top level domain names and mature
(registered for more than 30 days) non dynu.com third level domain names."* No
`TXT` means no DKIM. No DKIM means Resend cannot verify the domain. And since
Gmail and Yahoo tightened sender requirements in February 2024, with Microsoft
following in May 2025, unauthenticated mail does not reliably land anywhere.

This matters more here than in most products, because **mail is not a feature of
this one, it is the entry to it**. `POST /auth/sign-up` answers `202` and mails a
link; the household does not exist until someone clicks it. Without mail, nobody
can create an account at all — not a customer, not the owner.

Alternatives examined and rejected:

| Option | Why not |
|---|---|
| Supabase Auth's built-in mail | Not a relay — it only sends Supabase Auth's own mail, so our Go service has nowhere to point. Capped at 2 messages/hour per project, best-effort, and refuses any address not on the project team. Supabase's own docs require custom SMTP for production, which needs a domain again |
| Brevo without domain authentication | Technically sends, but rewrites the sender address to one of its own; Brevo documents this as a stopgap for testing, not production |
| FreeDNS (afraid.org) | Does allow `TXT` and `MX` on free subdomains, so it would work mechanically. Its parent domains are shared and mostly absent from the Public Suffix List, so the parent's DMARC policy governs, and many carry existing spam reputation |
| eu.org | The technically correct free answer — a real domain, on the Public Suffix List, full nameserver delegation. Manual volunteer approval takes days to months, so it cannot unblock this week. **Worth applying for in the background** |
| Buying a domain (~US$11/year) | Not rejected. Deferred — see the exit condition |

## Decision

**Run Mailpit in production and read the inbox by hand.** Sign-up, invites,
magic links and the lockout recovery path all work exactly as designed; the mail
simply never leaves the machine.

Mailpit's web UI is bound to `127.0.0.1:8025` on the host, never `0.0.0.0`, and
is reached through an SSH tunnel:

```bash
ssh -L 8025:127.0.0.1:8025 deploy@<box>   # then http://localhost:8025
```

The alternative we refused was faking deliverability — pointing at a relay that
would send unauthenticated mail into spam folders and calling that done. This
product's own conventions already refuse that shape twice: Budget spec decision 1
and Goals decision 4 both rejected a setting that *looks* automatic but is not.
Mail that looks delivered but isn't is the same dishonesty.

## Consequences

**The install is genuinely two-person.** Nobody outside can sign up or recover an
account without the owner opening Mailpit and relaying the link by hand. That is
acceptable for a private soft launch (see the deployment spec's decision 1) and
unacceptable the moment there is a third user.

**Mailpit's inbox is a complete authentication bypass.** Every magic link in it
grants full access to an account with no password. Exposing port 8025 publicly
hands over every account on the install. The loopback binding is a security
control, not a convenience — `docker-compose.prod.yml` says so at the line.

**TLS is unaffected.** Caddy uses the HTTP-01 challenge over port 80, which needs
no DNS `TXT` record, so `oink.mywire.org` gets an ordinary browser-trusted
Let's Encrypt certificate. The DDNS restriction bites only on mail.

**`SMTP_TLS_MODE=none` has to be set explicitly**, because it defaults to
`mandatory` outside development and Mailpit speaks plain SMTP. That line must be
deleted when a real relay arrives, or it would downgrade a connection that
should be encrypted.

**The verification walk changes meaning.** Criterion 3 (mail arrives in a Gmail
inbox, not spam) cannot be exercised at all — nothing leaves the box, so there is
nothing to receive. It is deferred, not passed, and is the criterion that tells
you whether real mail works, so it must be run the day a domain lands.

Criterion 2 (sign-up from a phone on mobile data) *is* still runnable, in reduced
form: the submission runs from the phone and must answer `202`, and the link is
then collected from Mailpit rather than the phone's inbox. **Run it that way
rather than skipping it.** Criterion 10 — the per-IP limiter keying on the real
client rather than on Caddy — turns on the phone having been established as a
genuinely separate client, and criterion 2 is what establishes that. Dropping it
would quietly remove the basis for the one criterion that proves a security
control.

*(Corrected 2026-08-15: this consequence originally deferred criterion 2 along
with criterion 3, which would have left criterion 10 resting on nothing.)*

## Exit condition

**The day a person who is not Andreas or Christine needs to receive an email.**
Not a date, not a milestone — that event.

The switch is small, which is ADR 1's exit-cost principle paying off in practice:
four values in `deploy/.env` (`SMTP_ADDR`, `SMTP_USERNAME`, `SMTP_PASSWORD`,
`SMTP_FROM`), deleting `SMTP_TLS_MODE=none`, and removing the `mailpit` service
from the Compose file. **No application code changes at all** — the mailer was
always configuration behind a port, so a hosted relay drops in where Mailpit was.

Changing `APP_BASE_URL` to a new domain at the same time will invalidate existing
sessions (the cookie origin moves) and break any link already mailed. At two
users that costs one sign-in each.

### Amended 2026-09-01 — that event is no longer the trigger

The original condition above is left standing because it records what was true
when this ADR was written: "a third person arrives" and "a third person needs an
email" were the same event. They stopped being the same event when
[ADR 4](0004-telegram-as-a-second-delivery-channel.md) added Telegram as a
second delivery channel for sign-up links and magic links. A stranger can now
create a household, and an existing member can now recover an account, without
any mail leaving this box.

**This amendment qualifies three earlier passages here rather than deleting
them:**

- **Context**, "Without mail, nobody can create an account at all — not a
  customer, not the owner." True only of an install with no bot configured.
- **Consequences**, "The install is genuinely two-person." Narrowed the same
  way: with a bot configured, sign-up and magic-link recovery are self-service.
  Invites and notification preferences are not, so the install is still
  two-person for those.
- **The criterion-3 paragraph is untouched and still binding.** Nothing here
  makes real mail testable; criterion 3 is still deferred, not passed.

**The new exit condition: the day something Hearth must deliver cannot go over
Telegram.** Three known candidates, and the first is already real:

| Trigger | Live today? |
|---|---|
| **An invite to someone who is not on Telegram** | **Yes.** Invites deliberately stay on email in ADR 4's slice, so this trigger is already met the moment a third person is invited |
| **Any notification** — bill reminders, retro reminders | No, and dormant. Nothing in this codebase sends one on any channel yet (`docs/FEATURE_TRACKER.md`'s notifications correction), so there is nothing to fail to deliver |
| **A person who will not, or cannot, install Telegram** | Unknown until it happens. Telegram is free, but it is still an account with a third party, and "sign up for a messaging app first" is a real cost to put in front of a stranger |

**Nothing about the switch itself changed** — the four `.env` values and the
Compose edit above are still the whole of it. That is ADR 1's exit-cost
principle paying off twice now: adding a whole second delivery channel needed no
change to the mailer either.

## See also

- [ADR 1 — optimise for exit cost](0001-optimise-for-exit-cost.md)
- [ADR 2 — first production host](0002-first-production-host.md)
- [ADR 4 — Telegram as a second delivery channel](0004-telegram-as-a-second-delivery-channel.md),
  which amended this one's exit condition
- `docs/superpowers/specs/2026-08-10-hearth-production-deployment-design.md`,
  whose decisions 6 and 11 assumed Resend and are superseded for as long as this
  ADR stands.
