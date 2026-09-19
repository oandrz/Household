# Partner invite lobby, milestone 2 — handover

**Written 2026-09-20, for whoever picks milestone 2 up.** Milestone 1 is built,
reviewed, walked and open as [PR #27](https://github.com/oandrz/Household/pull/27)
on branch `partner-invite-lobby` (rebased onto `main`, 16 commits). Milestone 2
has a **PRD and a spec but no plan yet** — writing that plan is your first
coding-adjacent task, and §5 lists what must be settled before you write it.

Read in this order: the PRD, the spec, then this file. Everything below is
either a pointer into code that already exists or a decision already made, so
you do not have to rediscover it.

| Document | Path |
|---|---|
| PRD (all three milestones) | `.claude/prds/partner-invite-lobby.prd.md` |
| Spec (milestones 1 and 2, 15 decisions) | `docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md` |
| Milestone 1 plan | `docs/superpowers/plans/2026-09-19-hearth-partner-invite-lobby-m1.md` |
| Milestone 1 walk record | `docs/superpowers/plans/2026-09-19-hearth-partner-invite-lobby-m1-verification.md` |
| Milestone 2 plan | **does not exist — you write it** |

## 1. What milestone 2 is

An owner invites their partner and gets a **one-time Telegram link** to hand
over (copy, QR, share). The partner taps it; the bot records a **knock** and
shows them a four-digit code; the owner's browser shows the same code and asks
"Does their phone show 4812?"; the owner clicks **Let in**, which creates the
member in one transaction and sends them a sign-in link in their own chat.
Email invites are hidden behind a flag while mail cannot leave the box.

Why it matters: production mail never leaves the box
([ADR 3](../../adr/0003-mail-stays-on-the-box.md)), so today a customer's invite
email lands in the **operator's** Mailpit. Until this ships, a self-serve
household cannot get a second owner, and Agreements stays locked for them.

## 2. What milestone 1 already gives you

Milestone 1 deliberately built the surface milestone 2's knock appears in.

**Go**
- `usecase.InviteSummary` (`api/internal/usecase/ports.go`) — the list row.
  **Note the name:** `usecase.PendingInvite` was already taken by the admin
  directory, so the new type is `InviteSummary`. Likewise the handler DTO is
  `inviteSummaryDTO`, because `pendingInviteDTO` exists in
  `admin_directory_handlers.go`. Expect the same collision when you add types.
- `InviteRepository.ListPending(ctx, householdID, now)` and
  `.Delete(ctx, householdID, inviteID)` with their contracts in `ports.go`;
  Postgres implementation in `api/internal/adapter/postgres/invite_repo.go`;
  queries `ListPendingInvites`, `DeleteUnacceptedInvite`,
  `InviteAcceptedInHousehold` in `queries/identity.sql`.
- `InviteService.ListPending` / `.Withdraw` in `api/internal/usecase/invite.go`.
- Handlers in `api/internal/adapter/http/pending_invite_handlers.go`; routes in
  `router.go` (`GET /household/invites` under `requireOwner`;
  `DELETE /household/invites/{id}` under `requireCSRF` → `requireOwner` →
  `requireCookieSession`); tests in `pending_invites_api_test.go`.
- "Pending" has one definition everywhere: `accepted_at IS NULL AND expires_at >
  now`, with `now` from the caller's `Clock`.

**Frontend**
- `web/src/features/settings/PendingInvitesList.tsx` — the row component with
  Withdraw. **This is the component the knock UI grows into** (waiting state →
  knocked state with the code and Let in / Not them).
- `usePendingInvites.ts` — `pendingInvitesQueryKey = ["household", "invites"]`,
  `usePendingInvites({ enabled })`, `useWithdrawInvite()`. Milestone 2 adds the
  3-second poll here.
- `schemas.ts` — `pendingInviteSchema`. Milestone 2 adds `channel` and `knock`;
  keep them optional so a milestone-1 server still parses.
- `InviteMemberModal.tsx` — mounted only while open, seeded from a `defaultRole`
  prop. Partner entry points pass `?invite=partner` (validated fail-closed in
  `web/src/routes/router.tsx`) and open on **Parent**; Settings' "+ Invite"
  opens on **Kid**.
- `web/src/features/overview/partnerStep.ts` — `"none" | "invited" | "joined"`,
  counting owners only.

## 3. The Telegram machinery you will build on

**ADR 10's chat-linking flow is already knock-and-confirm.** Read it before
designing anything: a browser mints a nonce, the bot's `/start` records which
chat redeemed it and writes nothing, and the minting session confirms. Milestone
2 is the same shape with a different confirmer (an owner, not the chat's owner).

- `api/internal/usecase/telegram_auth.go` — `HandleStart(ctx, chatID, payload,
  username)`. Your `inv_` branch goes **before** `Links.Consume`.
  `telegramDeadLinkMessage` is the one bland refusal for unknown, expired,
  consumed and rate-limited links; `sendSignIn` mints the magic link you will
  reuse after Let in.
- `api/internal/usecase/telegram_link.go` — `Start` / `Status` / `Confirm` /
  `Binding` / `Unlink`, and `TelegramLinkService`'s polling contract, which the
  lobby's poll should mirror.
- Migrations `00011_telegram.sql` (`telegram_link_requests`,
  `telegram_accounts`) and `00018_telegram_link_user.sql`. **`00018`'s comment
  reserves the `inv_` payload prefix for exactly this work.**
- `api/internal/adapter/telegram/update.go` — `senderName` returns the
  `@username` or `""`, **never a first name**, deliberately. That is why the
  spec adds the four-digit matching code (decision 3), which no endpoint ever
  accepts.
- `api/internal/adapter/crypto/tokens.go` — `NewToken` is 32 random bytes,
  43 base64url characters. `inv_` + 43 = 47, under Telegram's 64-character
  `start` limit.
- `users.email` is already nullable and Telegram-only accounts already exist
  (`SignupRepository.Provision` writes one plus its `telegram_accounts` row in
  one transaction) — that is the model for Let in's transaction.
- Feature flags: `api/internal/domain/featureflag.go` is the whole registry
  (one const plus one line in `AllFlags()`), `requireFeature` gates routes, and
  `/me` already returns `Features` to the frontend. `email_invites` goes here,
  **default off** — `FlagNotificationDelivery`'s comment explains why a flag
  that is on for something impossible is a lie.

## 4. Decisions already made (spec §Decisions — do not relitigate)

1. Extend the `invites` table; no new table. `channel` plus four knock columns.
2. **One knock per link.** A second tap gets the dead-link reply; "Not them"
   kills the link and issues a new one.
3. The matching code is **display-only**; no endpoint accepts it (ADR 4 rejected
   typed one-time codes, and this is not one).
4. Let in happens only in the owner's signed-in browser session (ADR 10).
5. Let in is **one repository method, one transaction** (user, membership,
   `telegram_accounts`, accepted stamp).
6. The sign-in link is sent **after** the commit, and a failed send is named
   (`signInSent: false`), not hidden.
7. **The public web accept route must refuse Telegram invites** — otherwise a
   link holder accepts through the browser and skips Let in entirely. This is
   one of the spec's named mutation checks.
8. Telegram links live 24 hours; email invites keep 7 days.
9. An invite for someone who will sign in has exactly one channel.
10. `email_invites` flag, default off, enforced at the HTTP edge too.
11. Telegram invites need `telegram_sign_in` on and a bot configured.
12. Every route that creates or removes a way into the household needs a browser
    session. **Milestone 1 did this only for the invite routes** — `PATCH` and
    `DELETE /household/members/{id}` still accept a token, and the spec says they
    gain the guard in milestone 2.
13. Withdraw deletes the row (already built).
14. The role is fixed when the invite is created.
15. A chat already linked to any account cannot knock.

## 5. Settle these before writing the plan

1. **ADR 4 amendment or a new ADR 11?** Invites over Telegram are ADR 4's own
   deferred item, and hiding email changes ADR 3's consequences.
2. **Link lifetime** — the spec says 24 hours (the owner chose it); confirm it
   still holds.
3. **What the owner sees when the knocker has no `@username`** — the code covers
   identity, but the row still needs wording.
4. **Several knocks on one link** — spec decision 2 says one; the PRD still
   lists it as open. Close it in the plan.
5. **Role change at Let in** — spec says no.
6. **Existing pending email invites when the email option is hidden** — assumed
   to stay listed and withdrawable.

**Also read first:** `docs/reviews/2026-09-19-security-review.md` (uncommitted on
the owner's machine, with a matching uncommitted paragraph in
`docs/HANDOVER.md`). It reports three High issues, and two are in the path this
milestone builds on: Telegram group chats acting as the owner and receiving
sign-in links, and the admin mail viewer showing working magic links. A knock
design that ignores the group-chat finding would inherit it.

## 6. How the work is run here

Spec exists, so: settle §5 → **superpowers:writing-plans** → execute with
**superpowers:subagent-driven-development** (one implementer per task, a review
after each, a whole-branch review at the end). Milestone 1 ran that way and is
the shape to copy; its plan is the format that worked.

**Definition of done** (CLAUDE.md, and milestone 1 met it): `make lint && make
test` green, mutation checks named in advance and actually run, a
fifteen-criterion browser walk recorded in a verification file, and
`FEATURE_TRACKER.md`, `LEARNING.md` and `SYSTEM_DESIGN.md` updated in the same
change. Milestone 2's walk needs a **real second phone on the development bot**
(`@HearthOinkDevBot`, token in the local `.env` only — the production token was
unusable for a dev walk, see the Telegram commands record).

## 7. Traps milestone 1 hit, so you do not

- **`stubFetchRoutes` throws on any unregistered request.** The moment a
  component fetches a new URL, every existing test that renders it fails. Budget
  a step for registering the route across test files.
- **Name collisions in `usecase` and the HTTP layer** (see §2). `go vet` catches
  them late; grep first.
- **Ports added to a service must be wired in `cmd/api/main.go` *and* in
  `api_test.go`'s own `Deps` literal** (`docs/LEARNING.md` pattern 23). Milestone
  2 adds several (a chat notifier, a code generator, the bot username).
- **A screen that reads a derived figure needs its query invalidated by name**
  (pattern 22). Let in must refresh the members list and the Overview checklist,
  not just the invites list.
- **Editor diagnostics lag behind the tree.** Mid-task they reported missing
  sqlc methods that existed; `go build ./...` is the truth.
- **colima stops when the machine sleeps.** `colima start`, then `make up`
  (not `make dev`, which blocks tailing logs). Two Docker engines exist here:
  check `lsof -nP -iTCP:5173 -sTCP:LISTEN` before trusting localhost.
- **Restart the web container after the last frontend edit** before walking, or
  the walk runs stale modules — that already cost one walk in September.
- **The working tree carries other people's uncommitted files** (`docs/SKILL_TRACKER.md`,
  the security review and its `HANDOVER.md` paragraph). Stage by explicit path;
  never `git add -A`, `git add .` or `git add docs`.

## 8. Known gaps left open by milestone 1

None block milestone 2; each is a candidate to fold in while you are in the file.

- Reloading `/settings?invite=partner` reopens the modal (pre-existing
  "seeded, not bound" behaviour in `MembersPanel.tsx`).
- The pending-invite poll milestone 2 adds should also cover the emailed-invite
  case: an invite accepted in another browser reaches an open Settings tab only
  on the next navigation.
- `InviteSummary.CreatedAt` is unused outside tests; no Go test asserts that
  `capabilities` round-trips through the list route.
- Three older production comments still reference plan task numbers
  (`usecase/invite.go`, `ports.go` twice).
- Milestone 3 (the access list: API tokens and linked chats in one Settings
  area) is specified only in the PRD and gets its own small spec.
