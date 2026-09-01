# Hearth — platform admin surface, browser verification

Walked 2026-09-02 against the running dev stack (`hearth-api-1`, `hearth-web-1`,
`hearth-postgres-1`, `hearth-mailpit-1`) at <http://localhost:5173>, on branch
`admin-surface`, schema version 12.

**Result: 15 of 15 criteria pass.**

Every criterion below was exercised in a real browser or against the real API
through the browser's own session, not in a test double. Where a criterion is
easier to prove at the API layer than by eye — a status code, an audit row —
both are recorded.

---

## The criteria

| # | Criterion | Result |
|---|---|---|
| 1 | A signed-in **non-admin** typing `/admin` sees the ordinary not-found screen, and the network answers **404**, not 403 | ✅ |
| 2 | The non-admin's sidebar has no Admin link | ✅ |
| 3 | The admin's sidebar has one | ✅ |
| 4 | Clicking it asks for the password before showing anything | ✅ |
| 5 | The wrong password is refused and the surface stays closed | ✅ |
| 6 | Three wrong passwords lock the admin surface — and household password sign-in still works | ✅ |
| 7 | `adminctl unlock-admin` reopens it | ✅ |
| 8 | The right password opens `/admin/flags`, listing every flag with its description and default | ✅ |
| 9 | Turning `family_calendar` on globally makes `GET /api/v1/family/calendar` answer 200 | ✅ |
| 10 | Overriding it **off** for one household closes it for that household only | ✅ |
| 11 | Deleting that override reopens it — "no opinion" differs from "explicitly off" | ✅ |
| 12 | Turning `signups_open` off makes the public sign-up POST answer 404; back on restores it | ✅ |
| 13 | An expired grant shows the password prompt again rather than signing the operator out | ✅ |
| 14 | `admin_audit_log` grows by exactly one for a plain page view | ✅ |
| 15 | Signing out closes the admin surface | ✅ |

## What each one actually showed

**1–3 — the surface is invisible to everyone else.** With the operator's
platform-admin row revoked (`adminctl revoke-platform-admin`), `/auth/me`
returned `isPlatformAdmin: false`, `GET /api/v1/admin/flags` returned
`404 {"error":{"code":"NOT_FOUND","message":"That endpoint does not exist."}}`
— byte-identical to any unrouted path — and `/admin` rendered the app's
ordinary "Page not found." with no admin chrome. After
`adminctl grant-platform-admin`, the Admin link appeared in the sidebar.

**4–5 — re-authentication.** The link leads to "Confirm your password /
Re-enter your password to open the admin surface." A wrong password left the
prompt in place with an inline error; the surface never rendered.

**6 — the two ledgers are genuinely separate.** After three failures, the
**correct** password was refused with `423 ADMIN_LOCKED`. In the same session,
household password sign-in returned **200**. This is the defect the separate
`admin_reauth_attempts` table exists to prevent, and it holds in the real
system: an operator fumbling the admin password cannot lock their household out
of the product.

**7 — the escape hatch is real.** `adminctl unlock-admin --email=` emptied
`admin_reauth_attempts` (0 rows), after which re-auth returned 204 and the flags
route 200. The comment in `usecase/admin_reauth.go` that names this command as
the way back in is now a checked claim rather than an assertion.

**8 — the flags screen.** Distinct operator chrome ("Hearth · Operator", dark
bar), all four flags with key, description and compile-time default.

**9–11 — the three states.** Toggling `family_calendar` on globally made the
gated route answer `200 {"events":[]}` and flipped `features.family_calendar` in
`/auth/me`. A household override set to **off** closed the route (404) for that
household while the global stayed on. Removing the override through the screen's
own "Remove" control reopened it (200) — proving deletion is not the same as
setting false.

**12 — closing registration.** With `signups_open` off, `POST /auth/sign-up`
answered 404, **and so did the token completion route** — a half-finished
sign-up cannot be redeemed after registration closes. Back on: 202.

**13 — the grant expires without signing you out.** With
`admin_grant_expires_at` set into the past, the admin route answered
`401 ADMIN_REAUTH_REQUIRED` while `/auth/me` stayed 200, and the browser showed
the password prompt at `/admin/flags` — it did **not** bounce to `/sign-in`.
This is the behaviour the frontend's admin-layer carve-out in `api/client.ts`
exists for, confirmed end to end.

**14 — auditing.** `admin_audit_log` went 18 → 19 for a single read. Rows carry
`action` and `target` populated (`GET /api/v1/admin/flags`), `detail` `{}`, and
an `ip`.

**15 — sign-out revokes the grant with the session.** After signing out, the
admin route answered `401 UNAUTHENTICATED` — not `ADMIN_REAUTH_REQUIRED` —
confirming the grant died with the session rather than outliving it.

---

## Findings from the walk

None of these failed a criterion. All are real, and the first two are worth
acting on before this reaches a user.

**A. There is no way to create a household override from the admin screen.**
The page offers a global On/Off control per flag, and a "Remove" control on
overrides that already exist — but nothing that creates one. The override in
criterion 10 had to be created with a `PUT` by hand. Per-household targeting is
the entire justification for the "global default with per-household override"
model, and it is currently reachable only by a hand-written API call. This is
the largest gap the walk found.

**B. The page's only interactive control is close to invisible.** The
segmented control is 12px text in `rgb(109,106,98)` on a transparent ground
inside a hairline border, right-aligned roughly 1900px from its own label at a
2246px viewport. It did not register at all on a first read of the screen; its
presence had to be confirmed through the accessibility tree and
`getBoundingClientRect`. The related deferred item — that the "Default" segment's
current-ness is carried by a background colour alone, with no `aria-pressed` —
compounds it: the operator cannot easily tell which of the three states a flag
is in, and the effective global value appears nowhere in the row's text.

**C. The re-auth error borrows sign-in copy.** A wrong password produces "That
email or password is incorrect." on a screen that has no email field. The code
is correct (`INVALID_CREDENTIALS`); only the wording is wrong for this context.

**D. A locked operator cannot see the lock without extending it.** On reload
while locked, the surface answers `ADMIN_REAUTH_REQUIRED` first, so the UI shows
the password prompt. The lock is only revealed by submitting — and a submission
while locked records another failure, pushing the expiry further out. The
lockout screen itself is good once reached (it names both doors: waiting it out,
or `adminctl unlock-admin`, and offers "Try again").

**E. In development the audit `ip` is the proxy's container address**
(`172.22.0.5:…`), not the client. Expected here — the real-IP configuration
lives in `web/nginx.conf` for production — but it means the dev audit log's `ip`
column is not meaningful.

## State changed on the dev box

- `platform_admins` now holds `andreas@hearth.family` (granted during the walk,
  left in place — it is what makes `/admin` reachable in development).
- `feature_flags` and `household_feature_flags` were emptied at the end, so
  every flag is back on its compile-time default.
- `admin_audit_log` carries the walk's own rows, deliberately: the table is
  append-only and has no delete path anywhere in the product.
- The dev Postgres was migrated 11 → 12 during Task 9 to apply this branch's own
  `00012_admin.sql`.
