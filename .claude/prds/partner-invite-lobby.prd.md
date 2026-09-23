# Partner invite lobby and household access list

## Problem

An owner who signs up for Hearth on their own cannot bring their partner in.
The Invite modal closes as though the invite was sent, but production email
never leaves the box ([ADR 3](../../docs/adr/0003-mail-stays-on-the-box.md)),
so the invite lands in the operator's own Mailpit and the partner never
receives it. Settings shows no trace of the invite, so the owner cannot tell
that it failed, and cannot resend or cancel it.

The cost is larger than one missing email. **Agreements stays locked until a
household has a second owner**, so every self-serve household loses a whole
Marriage feature. The only workaround is the operator reading Mailpit and
forwarding the link by hand, which means a stranger has to contact the operator
and the operator sees their invite link.

## Evidence

- **Confirmed in the code, not assumed:** an invite's accept link is handed only
  to the mailer. In production the mailer is Mailpit on the box. The only "Copy
  link" button anywhere in the product is on the operator's mail inspector
  screen.
- **Confirmed in the ADRs:** ADR 3's amended exit condition names "an invite to
  someone who is not on Telegram" as its trigger and records it as **already
  met**. ADR 4 moved sign-up and magic links to Telegram but deliberately left
  invites on email.
- **Confirmed in the tracker:** `docs/FEATURE_TRACKER.md` already carries three
  ⬜ rows this work closes: Telegram invites (a shareable `t.me/…?start=inv_…`
  link), Manage API tokens in Settings, and the Overview checklist's "Invite your
  partner" step, which "joins the list in the change that exposes pending
  invites". The Telegram-invites row justifies deferral as "one person's
  inconvenience on a two-person install". That stopped being true when sign-up
  became self-serve and open to anyone.
- **Zero real occurrences, because the product has not been shared yet.** As of
  2026-09-19 the product owner is the only person using the production install.
  This is a **launch blocker, not a reported pain**: the first stranger who
  invites a partner hits it on day one. Validate after launch via `/admin`
  household metrics (member count per household).

## Users

- **Primary:** the **owner of a self-serve household** who has just signed up
  and wants their partner inside. The trigger is the first session, usually with
  the partner sitting next to them.
- **Secondary:** any **owner asking "who can get into my household right
  now?"**: pending invites, API tokens and linked Telegram chats, in one place.
- **Not for:** limited members. They cannot invite, and they must not see other
  members' access.
- **Not for:** a partner who does not use Telegram. With email hidden (see
  Scope) there is no path for them until real email exists. This is a known,
  accepted gap, not an oversight.
- **Not for:** the operator. Nothing in this feature adds operator visibility
  into a household.

## Hypothesis

We believe a **one-time Telegram link that the partner taps and the owner
approves** ("knock", then "Let in") will let **an owner of a self-serve
household** bring their partner in **without any help from the operator**.

We'll know we're right when **a test partner on a second phone joins a fresh
household with zero operator steps and no Mailpit involved**, and, after
launch, when **the first household that is not the product owner's gains a
second owner without the operator touching anything**.

## Decisions already made (2026-09-19, by the product owner)

| # | Decision | Why |
|---|---|---|
| 1 | **The partner joins through a Telegram link plus the owner's "Let in"**, not by buying a domain for real email | $0 cost. Couples are usually in the same room, so tap or scan followed by an owner's approval is the natural path. The approval adds an owner check that email invites never had. Real email stays a separate ADR 3 decision |
| 2 | **Hide the email invite option while email cannot leave the install** | A stranger should never be offered an option that silently fails. It must come back without a code change on the day real email is configured. **It ships together with milestone 2**; hiding it earlier would leave no way to invite anyone |
| 3 | **In the access list, owners see every grant but revoke only their own**, plus any pending invite | ADR 7 makes API tokens personal. Cutting someone else's access stays "remove member", which already revokes everything that member holds. No ADR change needed |
| 4 | **Lobby first, access list later.** Three milestones, each shippable alone | The launch blocker is fixed before the convenience work |

## Requirements

What must be true. How it is built belongs to `/plan`.

**Joining (milestone 2)**

1. After creating an invite, the owner sees the invite waiting to be accepted
   instead of the modal closing. They get a link they can copy, show as a QR code
   for a phone in the same room, or share to Telegram.
2. The link is **shown once**. The product never displays it again. "Get a new
   link" issues a fresh one and **makes every earlier copy dead**.
3. Tapping the link does **not** put anyone in the household. It records a
   **knock** (who is asking, as Telegram identifies them), and the partner is
   told only that they are waiting to be let in. **No household name, member
   name or figure crosses Telegram** (the rule ADR 4 already sets).
4. The owner sees the knock in the browser and chooses **Let in** or **Not
   them**. Only **Let in** creates the member, with the role chosen when the
   invite was created. The partner then receives an ordinary sign-in link on
   their own chat.
5. **Not them** refuses that knock. The owner can then get a new link.
6. Creating a link, getting a new one, and Let in each **require a signed-in
   browser session. A personal API token must not be able to do any of them**,
   so a leaked token cannot mint a permanent co-owner (the reason behind ADR 7
   rule 2).
7. A Telegram chat already linked to a Hearth account is refused, with a plain
   explanation. **One account belongs to exactly one household.**
8. An unrecognised or expired link gets the same dead-link reply as any other
   stale Telegram link.
9. While real email is off, the invite form offers no email option. Limited
   members, who never had an email, are unaffected.

**Seeing invites (milestone 1)**

10. Settings lists each pending invite: who, which role, and when it expires.
    Any owner can withdraw one. A withdrawn or expired invite can no longer be
    accepted.
11. The Overview setup checklist gains **"Invite your partner"**. It counts as
    started while an invite is pending and done once a second owner exists. The
    Agreements "Invite your partner" link leads to the same place.

**Access list (milestone 3)**

12. One Settings area lists every live way into the household: pending invites,
    personal API tokens (name, created, last used, expiry) and linked Telegram
    chats, each labelled with its member.
13. Owners see all of it. **Each owner revokes only their own tokens and chat,
    plus any pending invite** (decision 3).
14. Creating an API token shows its secret once, the same rule as the invite
    link.
15. Limited members see only their own grants.

## Success Metrics

| Metric | Target | How measured |
|---|---|---|
| Operator steps per partner join | 0 | Dry run: fresh household, second phone |
| Time from invite to partner inside, same room | Under 2 minutes | Dry run, timed |
| Invites that appear sent but never arrive | 0 | Every invite path either delivers or says plainly why it cannot |
| Self-serve households with 2+ owners within 7 days of sign-up | TBD, needs a baseline after launch | `/admin` household metrics |

## Scope

**MVP (the hypothesis is tested at milestone 2):** pending invites visible and
withdrawable, then a one-time Telegram link, a knock, and the owner's Let in.
Email invites are hidden while email cannot leave the box.

**Out of scope**

- **Buying a domain and turning on real email.** A separate ADR 3 decision.
  When it happens, the email option reappears (decision 2).
- **Revoking another member's token or chat from the access list.** It would
  need an ADR 7 amendment, and it creates a new way for one partner to lock the
  other out.
- **A list of signed-in devices and "sign out the others".** A natural fourth
  row for the access list later. Not needed to test the hypothesis.
- **Invites over WhatsApp, SMS or anything other than Telegram.**
- **More than one household per account.** Settled at sign-up; unchanged.
- **Operator access visibility and household separation.** Raised in the same
  brainstorm (2026-09-19); each needs its own PRD.

## Delivery Milestones

| # | Milestone | Outcome | Status | Plan |
|---|---|---|---|---|
| 1 | Owners can see pending invites | Settings lists pending invites with role and expiry, and any owner can withdraw one. The Overview checklist gains "Invite your partner". No change to how an invite is delivered or accepted | **built, walked 2026-09-19** | [plan](../../docs/superpowers/plans/2026-09-19-hearth-partner-invite-lobby-m1.md) |
| 2 | Partner joins over Telegram | The owner gets a one-time link (copy, QR, share). The partner taps it, the knock appears, the owner lets them in, and the partner gets a sign-in link. The email invite option is hidden while email cannot leave. **Hypothesis tested here** | **built 2026-09-20; not yet merged, deployed, or walked in a browser** | [plan](../../docs/superpowers/plans/2026-09-20-hearth-partner-invite-lobby-m2.md) |
| 3 | Household access list | One place listing pending invites, API tokens and linked chats. Owners see all and revoke their own. Closes the tracker's "Manage API tokens in Settings" row | **built, gate and mutation checks recorded 2026-09-23; not yet merged, deployed, or walked in a browser** | [plan](../../docs/superpowers/plans/2026-09-23-hearth-household-access-list-m3.md) |

Each milestone ends with the project's usual definition of done: `make lint &&
make test` green, a mutation-checked test, a browser walk, and
`docs/FEATURE_TRACKER.md`, `docs/LEARNING.md` and `docs/SYSTEM_DESIGN.md`
updated.

## Open Questions

- [x] **ADR 4 amendment, or a new ADR 11?** Invites over Telegram are ADR 4's
      deferred follow-up. The decision to hide email invites also changes ADR
      3's consequences. Settle before milestone 2's spec.
      **Decided: a new ADR, [0011-joining-a-household-by-knock.md](../../docs/adr/0011-joining-a-household-by-knock.md),
      2026-09-20 — it extends ADR 10's reasoning to a second flow, which an
      amendment to ADR 4 could not carry on its own. ADR 4 and ADR 3 each
      gained a short amendment pointing at it instead.**
- [x] **How long does a link stay valid?** Email invites last seven days today.
      TBD: seven days to match, or shorter because the partner is usually in the
      same room. Validate in the milestone 2 spec.
      **Decided in the milestone 2 spec, decision 8: Telegram invite links
      live 24 hours; email invites keep their existing seven days.**
- [x] **What does the knock show the owner when the partner has no Telegram
      @username?** Telegram usernames are optional. TBD: the first name alone
      may not be enough to tell "Not them" apart. Validate by looking at what
      Telegram actually sends for an account without a username.
      **Decided: never the first name (it is attacker-chosen and not
      unique, `adapter/telegram/update.go`'s `senderName`) — the knock line
      reads "Someone with no Telegram username tapped the link" instead
      (`copy.ts`, `knockLine`), and the same rule applies to ADR 10's own
      confirm screen.**
- [x] **Several knocks on one link** (it was forwarded, or someone was fast):
      show every knock and let the owner pick one? TBD in the milestone 2 spec.
      Letting one in must consume the link either way.
      **Decided in the milestone 2 spec, decision 2: one knock per link.**
      The first `/start` with a link records it; every later tap gets the
      standard dead-link reply. There is no `invite_knocks` table because
      there is never more than one knock to hold. A leaked link costs one new
      link ("Not them" / "Get a new link"), never a takeover — see
      [ADR 11](../../docs/adr/0011-joining-a-household-by-knock.md).
- [x] **Can the owner change the role at Let in**, or only the role chosen when
      the invite was created? The MVP assumes it is fixed at creation.
      **Decided in the milestone 2 spec, decision 14: fixed at creation.**
      Let in grants exactly what the invite says; changing a role afterwards
      is an ordinary member edit, not part of this flow.
- [x] **Existing pending email invites when the email option is hidden.** Do
      they stay listed and withdrawable (assumed yes), or are they withdrawn
      automatically?
      **Decided: they stay listed and withdrawable.** `email_invites` gates
      only the *creation* of a new email invite, at the HTTP edge and in the
      invite modal; `ListPending`/`GET /household/invites` reads every
      pending invite regardless of channel or flag state, so a household with
      an email invite already in flight when the flag turns off keeps seeing
      and can still withdraw it.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| **The owner lets in the wrong person.** Whoever holds the link can knock | Medium | High: that person becomes an owner | Knock shows Telegram's identity for the person; nothing happens without Let in; the link is single-use, can be replaced, and expires; Not them refuses the knock |
| **A leaked API token creates links or lets someone in** | Low | High: a permanent co-owner | Requirement 6: those actions need a browser session. Tested per route |
| **A partner without Telegram cannot join** | Medium, outside Asia especially | Medium: that household stays single-owner, and Agreements stays locked | Accepted and documented under Users. Real email (ADR 3) is the way out |
| **Hiding email ships before the Telegram link** | Low | High: nobody can invite at all | Decision 2: hiding ships in milestone 2, never milestone 1 |
| **The Telegram bot is down or misconfigured on an install** | Low | Medium: no way to invite | The invite form says plainly that inviting is unavailable instead of offering a link that goes nowhere. TBD in the milestone 2 spec |
| **The knock leaks household details to a stranger's chat** | Low | High: a privacy breach | Requirement 3: the reply carries no household or member information |

---
*Status: Decisions 1-4 made by the product owner on 2026-09-19. Milestones 1
and 2 are planned and built (see Delivery Milestones above and
[ADR 11](../../docs/adr/0011-joining-a-household-by-knock.md)); all six open
questions above are settled. Milestone 3, the household access list, is
planned and built (see Delivery Milestones above,
[spec](../../docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md)
and
[plan](../../docs/superpowers/plans/2026-09-23-hearth-household-access-list-m3.md));
its browser walk has not run yet.*
