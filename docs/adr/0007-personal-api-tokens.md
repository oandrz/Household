# 7. Personal API tokens — a member's credential for headless callers

**Status:** Accepted — 2026-09-08. Walked against the dev stack the same day.

## Context

[ADR 6](0006-a-cli-as-the-automation-surface.md) chose a CLI over the
existing API as the automation surface and deferred API tokens "until a
headless machine, or a second agent, needs a credential that is not a
person's session". The product owner then asked for the automation roadmap
to be built through: a scheduled agent that runs with nobody at a keyboard
is exactly that trigger, and a 30-day browser session obtained by typing a
password is the wrong credential for it.

## Decision

A **personal API token** is a member's own long-lived credential. It names
the user and the household like a session does, and a request it
authenticates gets exactly the Scope a session would — the same membership,
role, capabilities and flags. There is one authorisation model, not two.

The rules that make it safe, each of which is a test:

1. **Bearer wins, and fails closed.** When an `Authorization` header is
   present it is the only credential considered. A bad token beside a good
   cookie is a 401; there is no fallback. A caller who sent a token meant to
   use it.
2. **A token cannot mint a token, revoke a token, or sign out.** Those need
   a browser session (`requireCookieSession`), so a leaked token cannot make
   itself permanent and revocation stays a decision a person makes.
3. **A token cannot reach `/admin`.** The admin grant lives on a session
   row; a token request has none and lands on the non-admin 404.
4. **CSRF is skipped for a resolved token only.** A cross-site form can make
   a browser attach cookies but cannot attach an `Authorization` header, so
   the double-submit check protects nothing there. Every other request is
   checked as before.
5. **Expiry is required** (default 90 days, at most 365), and revocation is
   a stamp. A revoked or expired token is unfindable, not found-then-checked.
6. **Tokens die with the member.** Membership update and removal revoke
   tokens beside sessions, with the same documented asymmetry.
7. **The raw token is shown once.** Only its SHA-256 is stored; a listing
   shows a name and an eight-character prefix.

Rejected: a household-level "service account" token. It would be a second
identity with its own role and capability rules, and every guard would
need a second reading. A member's token that does what the member can do
needs none of that.

## Consequences

- `hearthctl login --token` (or `HEARTH_TOKEN`) is the headless sign-in.
  `hearthctl token create|list|revoke` manage tokens from a browser session.
- There is no Settings screen for tokens yet; the feature tracker carries
  it as a ⬜ row.
- `Scope.AuthVia` exists. Any switch over it refuses by default; a third
  credential kind cannot be added without every such switch being revisited.
- Sessions and tokens are now two rows in two tables that both mean "this
  person may call the API". `MemberService.revokeCredentials` is the one
  place both are reset; a third credential type is added there.
