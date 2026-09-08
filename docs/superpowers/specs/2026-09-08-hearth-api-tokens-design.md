# Personal API tokens — design

**Date:** 2026-09-08. **Status:** built in the same change as this spec, on
branch `hearthctl-agent`. ADR 6 deferred tokens "until a headless machine or
a second agent needs a credential that is not a person's session". The
product owner asked for the automation roadmap to be built through, and the
scheduled-agent stage that follows this one is exactly that trigger.

## Decisions

1. **A token is a member's credential, not a household's.** It carries the
   user and the household, and every request it authenticates gets the same
   Scope a session would: the same membership, role, capabilities and flags.
   There is one authorisation model, not two.
2. **Table `api_tokens`**: `id, user_id, household_id, name, token_hash
   (bytea UNIQUE), prefix, created_at, expires_at NOT NULL, last_used_at,
   revoked_at`. The raw token is `hearth_` + 43 URL-safe base64 characters
   (32 random bytes, the same generator sessions use); only its SHA-256 is
   stored. `prefix` is the first 8 characters after `hearth_`, so `token
   list` can say which is which without the secret.
3. **Expiry is required.** Default 90 days, at most 365. A token that lasts
   forever is a credential nobody rotates. Revocation is a stamp, like every
   other "gone" in this product; a revoked or expired token is unfindable by
   the live lookup rather than found-then-checked.
4. **Bearer wins, and fails closed.** `requireSession` looks at the
   `Authorization` header first. If it is present, it is the only credential
   considered: a malformed, unknown, expired or revoked token answers the
   same 401 as a missing session and never falls back to the cookie. A
   request that carries both a bad token and a good cookie is refused —
   a caller who sent a token meant to use it.
5. **`Scope.AuthVia`** says which credential spoke: `session` or `token`.
   Any `switch` over it has a refusing default. Handlers that needed the raw
   cookie (sign-out, the admin re-auth) answer a clean 4xx to a token.
6. **CSRF is skipped for a resolved token only.** A browser cannot attach an
   `Authorization` header to a cross-site form post, so the double-submit
   check protects nothing there; for every other request it runs exactly as
   before.
7. **A token cannot mint a token, and cannot reach `/admin`.** `POST` and
   `DELETE /auth/tokens` sit behind `requireCookieSession`, so a leaked
   token cannot make itself permanent. The admin grant lives on a session
   row, so a token request has none and lands on the same 404 a non-admin
   gets.
8. **Tokens die with the member.** `MemberService.Update` and `Remove`
   revoke a member's tokens in the same breath as their sessions, with the
   same documented asymmetry (the membership change stands even if the
   revocation fails, and the failure is reported).
9. **Tokens skip session extension** (they have their own expiry) and touch
   `last_used_at` at most once an hour, the same throttle sessions use.
10. **No Settings screen yet.** Tokens are created, listed and revoked from
    `hearthctl`; the tracker records the screen as a ⬜ row.

## Routes

| Route | Guard | Body |
|---|---|---|
| `GET /auth/tokens` | session (cookie or token) | — → `{"tokens":[{id,name,prefix,createdAt,expiresAt,lastUsedAt}]}` |
| `POST /auth/tokens` | cookie session + CSRF | `{"name","expiresInDays"?}` → 201 `{token, id, name, prefix, expiresAt}` — the only time the raw token is ever returned |
| `DELETE /auth/tokens/{id}` | cookie session + CSRF | — → 204; another user's token id is 404 |

## CLI

`hearthctl token create --name=<n> [--days=90]` prints the raw token once;
`token list`; `token revoke <id>`. `hearthctl login --token` (or
`HEARTH_TOKEN`) stores it in the same per-host credential file, and every
request then sends `Authorization: Bearer …` with no cookies and no CSRF
header.

## Testing

- HTTP: a token reaches a money route with the member's real capabilities;
  a token write succeeds without the CSRF header; a bad token beside a good
  cookie is 401; expired and revoked are 401; a token cannot `POST
  /auth/tokens`; removing the member kills the token; the list body never
  contains a raw token; a token on `/admin/*` gets the 404.
- Usecase: name required, lifetime bounds, revoke is user-scoped.
- Mutation-checked: the no-fallback rule and the CSRF skip.
- Real environment: `hearthctl token create`, `login --token`, an insert,
  the row in the ledger; `token revoke`, the next call exits 2.
