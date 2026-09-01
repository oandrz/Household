# 4. Telegram as a second delivery channel

**Status:** Accepted — 2026-09-01. **Walked and passed — 2026-09-01.** The
code shipped complete and reviewed, and stayed unwalked only until a
BotFather bot existed to walk it against; `HearthOinkBot` now has, and every
criterion in
`docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md` passed
against it. See "Consequences".

## Context

[ADR 3](0003-mail-stays-on-the-box.md) records why no mail leaves the
production box: `oink.mywire.org` is a free Dynu DDNS hostname, Dynu refuses
`TXT` records on free third-level hostnames under thirty days old, no `TXT`
means no DKIM, and no DKIM means no hosted relay will send for it.

That constraint is more expensive here than it would be in most products,
because **mail is not a feature of this one, it is the entry to it**.
`POST /auth/sign-up` answers `202` and mails a link; the household does not
exist until someone taps that link. A stranger who signs up today waits forever
for an email delivered to a machine they cannot reach, and sees no error at all,
because `SignupService.Request` is deliberately silent in every branch — the
silence that closes the enumeration oracle also hides the failure.

So the product needs a way to put a link in a stranger's hand that does not
depend on DNS, a domain, or a relay's willingness to vouch for one.

The realistic candidates were WhatsApp, SMS, and a Telegram bot.

### Alternatives examined and rejected

| Option | Why not |
|---|---|
| **WhatsApp Business Platform** | Two independent blockers. **Cost:** the free-tier that made it attractive — unlimited replies inside a 24-hour service window opened by the user — **ends on 1 October 2026**, after which service conversations are billed per message. A per-message price on the *sign-up* path is a price on acquiring a user, forever. **Access:** the Cloud API requires a verified Meta Business account, and business verification wants a legal entity with registration documents. Hearth is a side project with no company behind it |
| **SMS** (Twilio, MessageBird, AWS SNS, any of them) | Costs per message on day one and forever, with no free tier that survives contact with production. Singapore additionally requires sender-ID registration for A2P traffic. Same objection as WhatsApp's, without WhatsApp's free window ever having existed |
| **Do nothing — buy a domain and fix mail instead** (~US$11/year) | **Not rejected, and not mutually exclusive.** It is still ADR 3's fix and still the right one for invites and notifications. It was not enough on its own here: it costs money and a wait, and it does not remove the thing that actually goes wrong for a stranger — a sign-up link landing in a spam folder is still a link they never tap. Telegram was chosen *as well*, not instead |
| **`eu.org`** (the free-domain answer ADR 3 already examined) | Manual volunteer approval takes days to months. Worth applying for in the background; cannot unblock anything on a schedule |
| **A webhook — `POST /api/v1/telegram/webhook`** | Rejected in favour of `getUpdates` long-polling from inside the API process. A webhook would be a new **public, unauthenticated route** whose only guard is a shared secret header, in a codebase whose rule is that a route without its guard has no second line of defence. It would need a CSRF exemption, its own rate limit, its own enumeration review, and re-registration whenever the hostname changes — and on a free DDNS hostname, the hostname changing is a live possibility, not a hypothetical. Long-polling costs one goroutine and adds no inbound surface at all |
| **`telegram_user_id` as a first-class identity beside `users.email`** | Doubles the identity surface: two unique keys, two enumeration analyses, `adminctl` lookups reworked, and an account-merge question ("this chat and this address are the same person") the product does not need answered yet. Telegram carries a *link*; the identity it proves is still the household owner the sign-up created |
| **Cross-device handoff — the browser starts the flow and polls for completion** | The obvious shape, and it has an account-takeover hole with no credentials involved. See "Why the link comes back to the tapper" below |
| **A numeric confirmation code (OTP) instead of a link** | A short code is guessable, so it needs its own attempt limiting, its own lockout and its own timing analysis — a second security surface to review beside the one that already exists. The nonce in this design is a 32-byte `TokenGenerator` token, hashed at rest, single-use, ten-minute TTL: the same class of secret as a magic link, analysed once. **There is no numeric code anywhere in this design**, and that is the main reason it was preferred |
| **A `Notifier` port spanning email and Telegram, now** | See "Why no `Notifier` port" below |

## Decision

**Add Telegram as a second delivery channel for tokens Hearth already mints.**

`users.email` stays the identity key wherever a user has one. Telegram carries
the sign-up link and the magic link; consuming either runs the **existing**
handlers and issues the **existing** session. No new session-issuing code exists
anywhere in this change. `docs/SYSTEM_DESIGN.md` §5 draws the flow.

Three sub-decisions carry the reasoning that is not obvious.

### Why Telegram, in one sentence

It is the only messaging channel that is **free per message, forever**, with no
domain, no DNS record, no business verification and no legal entity — and a bot
token takes about ninety seconds to create.

### Why the link comes back to the tapper, not to the browser that asked

Tapping the bot's reply signs the user in **on the device holding Telegram**.
There is no polling endpoint and no session-status route.

The alternative — browser starts the flow, polls until the chat completes it —
fails to a takeover that needs no credentials at all. The nonce is minted by a
browser and then bound to *whichever chat redeems it*. So an attacker starts a
flow in their own browser, sends the resulting `t.me/…` link to a victim ("tap
this for me"), the victim taps it in good faith, and the attacker's browser is
now signed in as the victim. Nothing about that is detectable from either end.

Link-back closes it rather than mitigating it: because the session lands on the
device that taps, forwarding the link gives the forwarder nothing.

**The cost is real and accepted.** A desktop user needs Telegram Desktop or
Telegram Web on the same machine as the browser. Cross-device can be added
later, but only with a confirmation code the user types back into the
originating browser, binding both ends — which is a different feature with its
own security analysis, not a relaxation of this one.

### Why no `Notifier` port

`Mailer` is untouched. `AuthService` and `SignupService` are untouched. All four
`Mailer` send sites are untouched.

A `Notifier` port spanning both channels would mean editing the four send sites
inside the two services carrying the most careful security reasoning in this
repository, and would need a `Destination` union with a fail-closed switch — new
abstraction, threaded through the exact code where a mistake is most expensive,
to serve two implementations that do not yet have a shared caller.

`CLAUDE.md` is explicit that a port arrives when a second implementation **and**
a second caller genuinely exist. Here `SignupService` sends over email and
`TelegramAuthService` sends over Telegram; neither needs to choose. Extracting
the abstraction later, from two working adapters, is cheaper and better
informed than designing it now — the same argument that keeps `BankSyncProvider`
unbuilt.

**Where the chat binding is written, since it is genuinely not obvious.**
`SignupService.Complete` does not create the user itself: it calls
`SignupRepository.Provision`, whose transaction already runs `ConsumeSignup`,
`CreateHousehold`, `CreateUser` and `CreateMembership`. The `telegram_accounts`
insert is a **fifth statement inside that existing transaction**, and the chat
id is read from the signup row being claimed rather than passed in — the same
rule that repository already applied to the email address. The consequence is
better than expected: `SignupService` gains no new dependency and no new branch.
The row decides which channel it belongs to.

## Consequences

**Telegram is now a third party on the authentication and recovery path.** Every
sign-in and sign-up link sent this way is readable by Telegram, and every such
link grants an account. That is the same property Mailpit's inbox already has
for whoever can reach it, and it is accepted for the same reason: on a free DDNS
hostname the alternative is no delivery at all. What bounds it is that the links
are short-lived and single-use, that nothing else about a household ever crosses
this channel — no balances, no names, not even a household id, only a URL — and
that revoking the bot token with `@BotFather` is an immediate kill switch that
needs no deploy. `docs/INFRASTRUCTURE.md` carries this as a dependency row
rather than leaving it here.

**Exactly one process may call `getUpdates`, forever.** Telegram hands each
update to a single caller, so a second `api` replica would silently steal
updates and the symptom would be "Telegram sign-in works about half the time" —
no error, no log, just intermittent failure. This is now an **architectural
constraint on ever scaling this service horizontally**, and it is the price of
having rejected the webhook. If Hearth ever needs two API replicas, the poller
has to move out first: either into a single-instance worker, or onto the webhook
this ADR declined.

**It shipped unwalked, and now it is walked, and the tracker says so.**
`make lint && make test` were green and nine tasks of work were reviewed
before a bot existed to walk the flow against; `HearthOinkBot`, a real
BotFather bot, closed that gap on 2026-09-01. Every numbered criterion and
every unnumbered check in
`docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md` passed
against it — a stranger's `/start` provisioned a household with no email
address at all, and a bound chat's `/start` sent a sign-in link rather than
a second sign-up, the discriminating case that only a correct
`Accounts.ByChatID` lookup produces. `docs/FEATURE_TRACKER.md` now marks
both features ✅, and `CLAUDE.md`'s standing rule — test the product in a
real browser before calling it done — is met here, not waived. The walk
stays tracked in the repository as the record of how, not only that.

**The feature is off unless configured, and half-configured will not boot.**
`TELEGRAM_BOT_TOKEN` and `TELEGRAM_BOT_USERNAME` must both be set or both be
empty; `config.Load` refuses the middle, the same both-or-neither rule
`SMTP_USERNAME`/`SMTP_PASSWORD` already follow, because a half-configured
channel misbehaves silently. Both empty — which is what `deploy/.env` on the
production box says, and this change is not deployed there yet in any case —
means the route answers `404`, the poller never starts, `adminctl` is unaffected, and the
sign-in screen hides the control rather than offering a button that always
fails.

**ADR 3's exit condition changed, and this ADR is why.** "The day a person who
is not Andreas or Christine needs to receive an email" stopped being the trigger
the moment a third person could arrive without one. ADR 3 carries the amended
version; the short form is that the trigger is now the day something Hearth must
deliver *cannot* go over Telegram — which is already true of invites.

**A third table now grows without a `Prune`.** `telegram_link_requests` joins
`signups` and `login_attempts` as a table a stranger can grow without holding an
account, and unlike those two it is not in `adminctl prune`. The rows are small
and the per-chat and per-IP limits bound the rate, so this is a slow leak rather
than a hole — but it is a real gap, recorded here and in
`docs/SYSTEM_DESIGN.md` §6 rather than left to be discovered.

**Amended 2026-09-01, same day, from the whole-branch review's fix wave.** The
paragraph above is left standing because it records what was true when this
ADR was written a few hours earlier — the gap it names is now closed.
`PruneTelegramLinkRequests` joins `adminctl prune` alongside `signups` and
`login_attempts`, mirroring `PruneSignups`'s own retention condition exactly;
`docs/SYSTEM_DESIGN.md` §6 and §8 are updated to match.

**One deliberate spec deviation, recorded rather than buried.** The design spec
said `SignupPreview` gains a `Channel` field; the implementation also widens
`GET /auth/sign-up/{token}`'s JSON body to carry it. The frontend cannot render
the right create-household screen otherwise — a Telegram sign-up has no address,
and shipping an empty read-only email box would be the "looks automatic but is
not" shape this product has refused twice before.

## Out of scope

- **Invites over Telegram.** A shareable `t.me/…?start=inv_<token>` link is the
  natural follow-up — the delivery channel now exists, so it is a payload change
  to the same `/start` parsing rather than new infrastructure. Deliberately not
  in this slice; `docs/FEATURE_TRACKER.md` carries it as ⬜ so it is a gap on the
  map rather than an assumption.
- **Cross-device sign-in.** Needs a typed confirmation code binding both ends;
  see "Why the link comes back to the tapper".
- **A `Notifier` port.** Extract it when a third channel arrives, or when one
  caller genuinely has to choose between two.
- **Removing Mailpit.** `SMTP_ADDR` is required by `config.Load`, and email
  flows still serve every user who has an address — including every invite.

## Exit condition

**The day Telegram stops being free, stops being reachable, or stops being
acceptable to the people who have to use it.** Any of the three, and the same
answer applies: the channel is a delivery adapter behind a port, so it can be
replaced without touching `AuthService`, `SignupService` or `Mailer` — which is
the whole reason it was built as one. This is [ADR 1](0001-optimise-for-exit-cost.md)
applied to a dependency chosen precisely because it was cheap to leave.

A weaker second trigger: **the day this service needs a second replica.** That
does not retire Telegram, but it does retire the poller — see the `getUpdates`
consequence above.

## See also

- [ADR 1 — optimise for exit cost](0001-optimise-for-exit-cost.md)
- [ADR 3 — mail stays on the box](0003-mail-stays-on-the-box.md), whose exit
  condition this ADR amended
- `docs/superpowers/specs/2026-09-01-telegram-sign-in-design.md` — the design
  this was built from
- `docs/SYSTEM_DESIGN.md` §1 (containers), §2 (the adapter's two directions),
  §3 (ports), §4 (the route and its limits), §5 (the flow), §6 (the tables)
