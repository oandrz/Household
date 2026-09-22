# Hearth — household access list (partner invite lobby, milestone 3)

**Date:** 2026-09-23
**PRD:** `.claude/prds/partner-invite-lobby.prd.md`, items 12–15 ("Access list")
**Follows:** `docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md`
(milestones 1 and 2, merged as #27 and #29), which named this milestone as
out of scope and deferred it to its own spec.
**Touches:** [ADR 7](../../adr/0007-personal-api-tokens.md) (personal API
tokens), [ADR 8](../../adr/0008-authorisation-at-each-channels-inbound-edge.md)
(authorisation at each edge). Neither is contradicted; see "ADR check".

## The problem, in one paragraph

A household has three kinds of live way in besides a password: pending
invites, personal API tokens and linked Telegram chats. Milestone 1 made
invites visible. Tokens have a complete API but **no screen** — today the only
way to see or revoke one is `hearthctl`. Chats are visible only to their own
member. So an owner cannot answer the question "who and what can get into our
household right now?" without a terminal, and cannot see their partner's
forgotten token at all.

## What ships

One **Access** panel in Settings, backed by one new read route.

| Group | Rows | Actions |
|---|---|---|
| API tokens | Every live token in the household (owner) or only your own (limited), each labelled with its member | **Revoke** on your own rows only; **New token** dialog that shows the secret once |
| Linked chats | Every linked chat (owner) or only your own (limited), each labelled with its member | Your own row embeds the existing Connect / Unlink component, unchanged |
| Pending invites | Not repeated. One line — "2 pending invites — in Members" — linking to the list milestone 1 built | none here |

No new write route. No migration.

## Decisions

1. **Household-wide for owners, self-only for limited members** (PRD 13, 15).
   Chosen over a self-only screen because the point of the list is that an
   owner can spot a *partner's* stale grant.
2. **One Access panel; pending invites stay under Members** with a pointer
   line. This bends PRD item 12 ("one area lists every way in") into one list
   plus a pointer. Accepted because milestone 1's list sits beside the
   "+ Invite" button that creates its rows, and it was walked 15 of 15 there;
   moving it would redo that walk for no new capability.
3. **One read route, `GET /api/v1/household/access`.** Chosen over two routes
   (two guards to get right, a client-side join) and over widening the
   existing routes with `?scope=household` (one route with two guard rules
   keyed on a query parameter is the guard a later change silently drops).
4. **The server decides the scope, not the client.** Owner → every member's
   rows; limited → only the caller's rows. The handler reads the role from the
   request scope and calls a different service method; the frontend renders
   whatever comes back. One component, and a limited member who tampers with
   the client still gets only their own rows — this fails closed.
5. **Browser session required.** A request authenticated by an API token gets
   `403 SESSION_REQUIRED` (`requireCookieSession`). Precedent goes both ways —
   `GET /auth/tokens` and `GET /household/invites` accept a token,
   `GET /telegram` does not — and this route follows the stricter one because
   of what it discloses: a leaked token that could list **every member's**
   token prefixes, expiries and chat usernames gives an attacker a map of the
   household's other credentials, which a self-only read does not.
6. **Revoke reuses the existing routes.** `DELETE /auth/tokens/{id}` is
   already scoped to the token's owner in the repository (another user's id
   is `domain.ErrNotFound` → `404`), and `DELETE /telegram` unlinks the
   caller's own chat. **An owner cannot revoke another member's token or
   chat** — that would need an ADR 7 amendment, and the milestone 1–2 spec
   lists it as out of scope. The frontend shows Revoke / Unlink only on the
   caller's own rows; the `404` is the second line.
7. **Telegram off means no chats group.** When the `telegram_sign_in` flag is
   off for the household, or no bot is configured (`deps.TelegramLink == nil`),
   the response carries `"telegram_enabled": false` and `"chats": []`, and the
   panel hides the Linked chats group. The route itself stays available — the
   tokens half does not depend on Telegram.
8. **Flat lists, each row carrying its member.** Not nested per member: the
   panel groups by kind (tokens, chats), not by person, so flat is the shape
   it renders.
9. **The chat id never leaves the server.** Rows carry `chat_username`
   (nullable — Telegram users may have none) and `linked_at`, never
   `chat_id`. The panel shows `@username`, or "Telegram chat" when it is null.
10. **The existing Telegram connect flow is embedded, not rewritten.**
    `TelegramPanel`'s connect / poll / confirm / unlink behaviour moves inside
    the Access panel's own row unchanged; only its outer card and heading go.
    Its tests move with it.
11. **The new-token dialog reuses the shown-once pattern** of
    `InviteLinkShare.tsx` (PRD 14): the raw `hearth_…` secret appears once
    with Copy, and closing the dialog drops it from memory. Name is required;
    expiry offers 30 / 90 (default) / 365 days, matching ADR 7's bounds.

## Backend

### Route

```
GET /api/v1/household/access
  guards: requireSession → requireCookieSession
  owner   → AccessListService.ForHousehold(householdID)
  limited → AccessListService.ForMember(householdID, userID)
```

It sits in a group of its own in `router.go` with a comment saying why it is
cookie-only (decision 5). No CSRF guard is needed for a `GET`.

### Response (`200`)

```json
{
  "telegram_enabled": true,
  "tokens": [
    {
      "id": "…", "member_id": "…", "member_name": "Alex",
      "name": "laptop script", "prefix": "hearth_ab",
      "created_at": "…", "last_used_at": null, "expires_at": "…"
    }
  ],
  "chats": [
    {
      "member_id": "…", "member_name": "Sam",
      "chat_username": "sam_k", "linked_at": "…"
    }
  ]
}
```

Tokens newest first; chats by `linked_at`, newest first. Only **live** tokens:
revoked or expired rows never appear, the same rule as `ByTokenHash`.

### Usecase

`internal/usecase/access_list_service.go` — one file, one job: assemble the
list. No actor parameter (CLAUDE.md: services enforce what is valid, the edge
enforces who is asking).

```go
type AccessList struct {
    TelegramEnabled bool
    Tokens          []AccessToken // domain.APIToken + member display name
    Chats           []AccessChat  // member id, display name, username, linked at
}

func (s *AccessListService) ForHousehold(ctx, householdID string) (AccessList, error)
func (s *AccessListService) ForMember(ctx, householdID, userID string) (AccessList, error)
```

`ForMember` is `ForHousehold` filtered to one user, done in the service rather
than by a second pair of queries, so both paths share one query and one set of
repository tests. The member display name comes from the existing membership
listing the Members panel already uses.

`TelegramEnabled` is decided at the HTTP edge (flag resolution already lives
there) and passed in, not looked up by the service.

### Ports (`internal/usecase/ports.go`)

Two narrow additions, each with a doc comment stating its contract:

- `APITokenRepository.ListForHousehold(ctx, householdID) ([]domain.APIToken, error)`
  — live tokens only, newest first, never another household's.
- `TelegramAccountRepository.ListForHousehold(ctx, householdID) ([]TelegramBinding, error)`
  — `telegram_accounts` has only `user_id`, so the Postgres adapter joins
  through `memberships`. Never another household's.

Both return an empty slice, not `domain.ErrNotFound`, when there is nothing.

## Frontend

- `features/settings/AccessPanel.tsx` — the card and its two groups.
- `features/settings/ApiTokenList.tsx` — token rows; Revoke on own rows.
- `features/settings/NewApiTokenModal.tsx` — create, then show once.
- `features/settings/LinkedChatList.tsx` — chat rows; the caller's own row
  renders the extracted connect/unlink component.
- `features/settings/useHouseholdAccess.ts` — the query; `useApiTokens.ts`
  mutations for create and revoke, both invalidating the access query.
- `schemas.ts` gains the access response schema.
- `SettingsPage.tsx` drops the standalone `TelegramPanel` and renders
  `AccessPanel`.

"Own row" is decided by `member_id === me.id`, never by role.

Every visible string goes in `copy.ts`, the same as the other panels.

## Error handling

| Case | Answer |
|---|---|
| API token instead of a browser session | `403 SESSION_REQUIRED` |
| Not signed in | `401` |
| Telegram flag off / no bot | `200`, `telegram_enabled: false`, `chats: []` |
| Owner revokes partner's token id | `404` from the existing route; the panel refreshes |
| Revoke of an already-revoked token (two tabs) | `404`; the panel refreshes and the row is gone |
| Repository failure | `500` through `MapDomainError`, as everywhere |

## Tests

Tests first, and each named for the behaviour it shows.

**Repository (testcontainers, real Postgres)**
- a second household's tokens and chats never appear
- revoked and expired tokens are left out; a token expiring in the future is in
- a household with none returns an empty slice

**Usecase (in-memory doubles)**
- `ForMember` returns only that member's tokens and chat
- member names are attached to each row

**HTTP**
- an owner sees the partner's token and chat
- a limited member sees only their own, even when the partner has both
- a token-authenticated request is `403 SESSION_REQUIRED`
- the Telegram flag off gives `telegram_enabled: false` and an empty `chats`
- no response carries `chat_id` (asserted on the raw JSON)
- an owner `DELETE`ing the partner's token id gets `404`, and the token still works

**Frontend**
- Revoke appears on own rows only
- the new-token dialog shows the secret once and not after reopening
- the chats group is hidden when `telegram_enabled` is false
- the pending-invite pointer shows the count and links to Members

**Mutation checks (at least these)**
- serve `ForHousehold` to a limited member → the limited-member test goes red
- remove `requireCookieSession` from the route → the token test goes red
- drop the household filter from the token query → the second-household test goes red

**Browser walk** (written as criteria in the plan): owner and partner in two
sessions; mint a token as the partner, see it from the owner's panel with no
Revoke button; revoke it as the partner and watch it leave the owner's list on
refresh; mint one as the owner and copy the secret once; `hearthctl login
--token` with it; confirm a limited member's panel shows only their own rows;
check at 375px.

## Docs in the same change

- `docs/FEATURE_TRACKER.md`: rewrite "Manage API tokens in Settings" (it says
  "a frontend screen over existing endpoints" — no longer true) and move it to
  ✅; add a row for the household access list; recount the summary table.
- `docs/SYSTEM_DESIGN.md` (via `maintaining-system-design`): the new route and
  its guards, `AccessListService`, the two repository methods, and the
  Settings component change.
- `api/cmd/hearthctl` routes table: add `GET /household/access` — a test diffs
  the table against `router.go` both ways and goes red until it is there.
  Mention it in `docs/CLI.md`.
- `docs/LEARNING.md`: what the work taught.
- The PRD's closing note ("Milestone 3 … is not yet planned") updated.

## ADR check

- **ADR 7** — unchanged. Tokens are still minted and revoked only from a
  browser session and only by their own member; this adds a read. Its
  Consequences line "There is no Settings screen for tokens yet" becomes
  false: amend that line to point at this spec.
- **ADR 8** — respected. Authorisation (owner vs limited, cookie vs token)
  lives in the HTTP layer; `AccessListService` takes no actor.

## Out of scope

- Revoking another member's token or chat (needs an ADR 7 amendment).
- A list of signed-in browser sessions / devices — the natural next row for
  this panel.
- Editing a token's name or expiry after creation.
- Moving the pending invites list out of Members (decision 2).

## Milestone

| # | Ships | Schema | Visible change |
|---|---|---|---|
| 3 | `GET /household/access`; `AccessListService`; two repo methods; the Access panel with token create/revoke and the embedded chat flow | none | Owners see every live token and chat in the household; everyone can mint and revoke their own tokens without a terminal |

Done means the project's definition of done: `make lint && make test` green,
the mutation checks above run, the browser walk passed, and the docs above
updated.
