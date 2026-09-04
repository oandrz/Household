# Hearth — outbound message inspector, browser verification

Walked 2026-09-04 against the running dev stack (`hearth-api-1`, `hearth-web-1`,
`hearth-postgres-1`, `hearth-mailpit-1`) at <http://localhost:5173>, on branch
`admin-outbox`. Browser tool: Claude in Chrome connected on the first attempt
and was used for setup (sign-in, the magic-link request, sending the invite)
and criterion 1. Its own screenshot capture then proved unreliable on this
box partway through criterion 2 (see the tooling caveat below), so every
criterion from 2 onward — every screenshot, click, `fetch` and `evaluate`
call, the incognito-equivalent contexts, and the viewport matrix — used the
Playwright MCP tools instead, per the brief's standing permission to fall
back. `form_input` calls were also mis-issued from this session's harness,
the same malformed payload resurfacing verbatim across retries; worked
around with `javascript_tool` (setting values via the native property setter
plus an `input` event) rather than spending more attempts on it — recorded
here as a tooling note, not a product defect.

**Environment note found before the walk began, fixed without touching
product code:** `andreas@hearth.family`'s password did not match the
documented development default (`hearth-dev-password`) — a previous walk on
this same long-lived dev box (the admin-households verification) had changed
it to a throwaway value and never reset it. The very first sign-in attempt
failed ("That password doesn't match. Two tries left before we lock the
household for 15 minutes."). Rather than guess again and risk a 15-minute
household lockout before the walk even started, the password was reset to
the documented default via the sanctioned path,
`make reset-password EMAIL=andreas@hearth.family` (driven through `expect`
since its `term.ReadPassword` prompt needs a real TTY, which a plain piped
`docker exec` does not provide). This is environment upkeep, not a product
change, and is called out here so the state of the box is honest.

**Mail generated for the walk:** a magic-link request for
`andreas@hearth.family` (immediately after the reset, to get back in), a
second magic-link request made while re-establishing the session after the
"use a password instead" detour, and an invite sent from Settings to a fresh
address, `walk-2026-09-04@example.test` (role Parent, so it needs a real
inviteable email) — not to `christine@hearth.family`, who is already an
accepted member and would refuse a second invite. All three are genuine
product output, not mail placed into Mailpit by hand.

**Result: 15 of 15 criteria pass. One real defect was found and fixed during
the walk (criterion 14 — the operator header overflowed at 305px once this
task's own `Mail` link was added; fixed in `AdminShell.tsx`, re-verified,
and covered below). Caveat on criterion 8** (the exact hyphen/underscore
trailing-character case was proven at the unit-test level rather than
against a live clipboard, because this walk's own earlier magic-link
requests had already spent `andreas@hearth.family`'s hourly rate limit).

---

**Tooling caveat discovered mid-walk, before any criterion touching visuals
was recorded:** on this box, Claude in Chrome's `computer` screenshot/`zoom`
actions render the operator header's nav as showing only its first item
(`Flags`) — the other three links (`Mail`, `Households`, `Back to Hearth`)
are absent from the captured image even though `getBoundingClientRect`,
computed styles (forced to opaque red with a yellow outline as a direct
test) and the accessibility tree all agree they are present, laid out,
correctly coloured and not clipped by any ancestor. The tab's own
`window.devicePixelRatio` reads `2.5` and `outerWidth` reads `594` against an
`innerWidth` of `1209` — an inconsistent, non-integer scaling setup that
belongs to this sandboxed display, not to a real user's screen. Confirmed as
a capture bug rather than a rendering bug by opening the same signed-in
route in the Playwright MCP browser (a separate, unscaled context) and
screenshotting it there: all four links render normally, cleanly
distinguishable, in the same DOM order the source defines
(`AdminShell.tsx`'s `items` array — Flags, Mail, Households). From here on,
Playwright MCP is used for the rest of the walk in full — navigation,
clicks, `evaluate`, and every screenshot — rather than splitting interaction
and screenshotting across two tools, per the brief's standing permission to
fall back to it once the Chrome extension stopped being trustworthy for
this purpose.

---

## The criteria

### 1. `/admin` re-auth prompt appears; the correct password opens the surface

**Done:** Signed in as `andreas@hearth.family` with the (reset) development
password, then navigated directly to `http://localhost:5173/admin`.

**Seen:** The route settled on `/admin/flags` and rendered "Confirm your
password / Re-enter your password to open the admin surface." with a single
password field and a Continue button — no other admin chrome visible yet.
Entering `hearth-dev-password` and submitting replaced the prompt with the
Flags screen (header "Hearth · Operator", the three feature-flag rows).

**Result:** PASS.

**Caveats:** None on the criterion itself. See the environment note above the
criteria: the first attempt at this exact step, before the password reset,
correctly refused a stale password rather than accepting it — which is
itself evidence the check is real, not a caveat against it.

---

### 2. The operator nav shows Flags · Mail · Households, Mail visibly active on its own page

**Done:** With the surface open, read `AdminShell.tsx`'s nav order in source
first (`Flags`, `Mail`, `Households`, then `Back to Hearth`) — this is the
one place the task's own framing documents disagree with each other (the
task-9 brief says "Flags · Households · Mail"; the parent prompt says
"Flags · Mail · Households") — then navigated to `/admin/mail` and checked
both a screenshot and computed styles, not just presence.

**Seen:** Playwright's screenshot (`nav-check.png`, both this session's own
capture and the reference image below) shows, left to right: `Flags`,
**`Mail`** (bold, fully white), `Households`, `Back to Hearth` — the order in
`AdminShell.tsx`'s `items` array, which is the code the running app actually
uses. `getComputedStyle` on each `<a>` in `nav[aria-label="Operator"]`:

| Link | `aria-current` | `color` | `font-weight` |
|---|---|---|---|
| Flags | `null` | `oklab(... / 0.6)` (white, 60% opacity) | 500 |
| Mail | `"page"` | `rgb(255, 255, 255)` (solid white) | 600 |
| Households | `null` | `oklab(... / 0.6)` | 500 |
| Back to Hearth | `null` | `oklab(... / 0.7)` | 500 |

Both colour (opacity 0.6 vs. fully opaque) and weight (500 vs. 600) separate
the active link from the rest, the same two-signal pattern the households
walk verified for this exact nav component — not relying on `aria-current`
alone, and not the "invisible active link" defect that shipped once before
in this repository.

**Result:** PASS.

**Caveats:** The task framing's own inconsistency about the nav order traced
back further than the brief: `docs/SYSTEM_DESIGN.md` §7 itself documented
"Flags · Households · Mail" — stale relative to the code it describes, from
the same task (Task 8, per its own report) that added the `Mail` link.
Fixed here rather than left as a footnote, since keeping this document true
is this branch's own standing rule, not a separate task: `SYSTEM_DESIGN.md`
now reads "Flags · Mail · Households", matching `AdminShell.tsx`'s `items`
array, which is the code the running app actually uses.

---

### 3. `/admin/mail` lists the magic-link and invite messages, newest first

**Done:** With three real messages generated before the walk began (two
magic-link requests for `andreas@hearth.family`, one invite to
`walk-2026-09-04@example.test`), loaded `/admin/mail` and read the table.

**Seen:**

| To | Subject | Sent |
|---|---|---|
| `walk-2026-09-04@example.test` | Andreas invited you to Hearth | Sep 4, 2026, 10:30 AM |
| `andreas@hearth.family` | Your Hearth sign-in link | Sep 4, 2026, 10:29 AM |
| `andreas@hearth.family` | Your Hearth sign-in link | Sep 4, 2026, 10:27 AM |

"Showing 3 of 3." Order matches Mailpit's own `Created` timestamps read
directly from its API (`0X4A0g…` 02:30:45Z, `00fsrK…` 02:29:28Z, `79VrMT…`
02:27:29Z) — newest first, exactly as `usecase/admin_outbox.go`'s listing is
specified to return them.

**Result:** PASS.

**Caveats:** None.

---

### 4. A list row shows recipient, subject and time — and no body text

**Done:** Checked this two ways, not just by eye: read every `<tbody> <tr>`'s
`textContent` in the live DOM, and separately called
`GET /api/v1/admin/mail?limit=50` directly from the page's own origin (so it
carries the real session cookie) and inspected the JSON's keys.

**Seen:** Each row's full text content is exactly the recipient, the subject
(twice — once in a `md:hidden` mobile-only span, once in the desktop-only
cell, per `MailRow` in `AdminMailPage.tsx`) and the sent time; no other text
node exists in the row. The API response itself carries only
`["messages", "total", "truncated"]` at the top level, and each message
object only `["id", "to", "subject", "sentAt"]` — there is no body field to
leak even if a future edit forgot to omit it from the row markup, matching
the file's own comment that `AdminMailSummary`'s `.strict()` schema would
refuse a response that tried to add one.

**Result:** PASS.

**Caveats:** None.

---

### 5. The durability line is on the page

**Done:** Read the paragraph directly under the `/admin/mail` heading.

**Seen:** "Mailpit keeps these only until it restarts — a deploy or a reboot
clears them. Send a fresh link rather than looking for an old one." — present
on first load, not behind any interaction.

**Result:** PASS.

**Caveats:** None.

---

### 6. Opening a message shows its links and the plain-text body; the route resolves

**Done:** A full page navigation (`page.goto`, not a client-side `<Link>`
click) straight to `/admin/mail/0X4A0gyWq3bXzedW7NndRq` (the invite message),
so this is a cold load of the route tree, not a soft transition from an
already-mounted app shell.

**Seen:** The page rendered "Andreas invited you to Hearth", "To
walk-2026-09-04@example.test · sent Sep 4, 2026, 10:30 AM", a **Links**
section with one row —
`http://localhost:5173/invite/6C5huD3Mdl-nJHXHMpAN4TkfyB3l6uYupa2h_POP81U`
and a "Copy link" button — and a **Message text** section below it with the
full plain-text body ("Hi Walk Test, / Andreas has invited you to join their
household on Hearth. Follow this link to accept: / \<the same URL\> / If you
weren't expecting this, you can ignore this email."). The link appears once
in the Links section even though it also appears inline in the body text,
matching `ExtractLinks`'s de-duplication.

**Result:** PASS.

**Caveats:** None.

---

### 7. The link shown is the real one

**Done:** Requested one more magic link immediately before this criterion
(`POST /api/v1/auth/magic-link` for `andreas@hearth.family`), rather than
reusing one from the top of the walk — magic links are single-use and expire
in 15 minutes (`magicLinkTTL` in `usecase/auth.go`), so an old one would fail
for a reason that has nothing to do with the inspector. Opened it through
the real `/admin/mail` UI to read the link exactly as an operator would, then
tested both links this session generated (the fresh magic link, and the
invite from Settings), each in a **genuinely cookie-less browser context** —
`page.context().browser().newContext()` via Playwright, which is a stronger
guarantee of "private" than an incognito window (zero cookies of any kind
for the origin, not just no first-party storage carried over), since the
Chrome-extension browser shares the signed-in user's real profile and
cannot open a true incognito window for this check.

**Seen:**

- Magic link
  (`http://localhost:5173/sign-in/magic?token=enedqoWN694rTRO0a5JLguX37fTW2R4AWGSSgT1ysA0`)
  opened in the fresh context, redirected to `/`, and rendered the signed-in
  Overview screen for "Andreas & Christine" (sidebar showing Settings,
  Admin, the household's net worth tile) — a `hearth_session` cookie was
  present afterward. The link signs in for real.
- Invite link
  (`http://localhost:5173/invite/6C5huD3Mdl-nJHXHMpAN4TkfyB3l6uYupa2h_POP81U`)
  opened in a second fresh context and rendered "Oentoro / Andreas invited
  you in. / You'll share Money, Marriage and Family. / Joining as co-owner…"
  with Name/Password fields and "Accept & join household" — the real invite
  screen. Not accepted, per the plan's own note not to consume it
  unnecessarily; opening it is what this criterion asks for.

**Result:** PASS.

**Caveats:** None.

---

### 8. The copy control puts the URL on the clipboard whole

**Done:** On the fresh magic-link message's detail page, clicked "Copy
link", confirmed the button itself flips from "Copy link" to "Copied" (proof
`navigator.clipboard` was defined and the write did not silently no-op —
`AdminMailPage.tsx`'s `handleCopy` returns early with no feedback at all if
it is undefined, which would have been a real finding), then read the
clipboard back with `navigator.clipboard.readText()` (after granting the
context clipboard permissions) and diffed it against the link text rendered
in the DOM.

**Seen:** Clipboard content:
`http://localhost:5173/sign-in/magic?token=enedqoWN694rTRO0a5JLguX37fTW2R4AWGSSgT1ysA0`
— identical to the displayed text, both 85 characters, both ending in `0`.
No truncation, no whitespace added or removed.

This session's magic and invite tokens happened not to end in `-` or `_`
(the one-in-roughly-thirty-two case the domain code's own comment names as
the specific risk — `outbox_links.go`'s `trailingPunctuation` deliberately
excludes both characters), and `RequestMagicLink`'s per-address rate limit
(`magicLinkPerHourLimit = 3` in `usecase/auth.go`) was already spent by this
walk's own earlier requests, so a live link ending in either character could
not be forced within the hour without an artificial reset. That exact case
is covered directly at the unit level instead:
`api/internal/domain/outbox_links_test.go` carries
`"a token ending in a hyphen keeps its last character"` and
`"...an underscore keeps its last character"`, both re-run here and
passing:

```
--- PASS: TestExtractLinks/a_token_ending_in_a_hyphen_keeps_its_last_character (0.00s)
--- PASS: TestExtractLinks/a_token_ending_in_an_underscore_keeps_its_last_character (0.00s)
```

The frontend copy handler itself (`handleCopy` in `AdminMailPage.tsx`) does
no trimming of its own — it is a direct `navigator.clipboard.writeText(link)`
— so nothing between the extraction this test covers and the clipboard this
walk exercised can reintroduce a strip.

**Result:** PASS.

**Caveats:** The exact hyphen/underscore-ending case was verified at the
domain-test level rather than against a live clipboard, for the rate-limit
reason above — recorded rather than silently substituted.

---

### 9. `/admin/mail/notarealid22chars0` answers a refusal, not a server error

**Done:** Navigated to `/admin/mail/notarealid22chars0` (18 characters, so it
misses `mailpitIDPattern`'s 22-character shape) and separately to
`/admin/mail/AAAAAAAAAAAAAAAAAAAAAA` (22 well-formed characters that no real
message has), checking both the rendered page and the raw
`fetch()` response.

**Seen:**

| URL | Rendered | HTTP | Body |
|---|---|---|---|
| `.../notarealid22chars0` | Inline alert: "That is not a message id." | `400` | `{"error":{"code":"INVALID_ID","message":"That is not a message id."}}` |
| `.../AAAA…AAAA` (22 chars) | "Mailpit no longer holds this message. Its store is cleared whenever the container restarts…" | `404` | `{"error":{"code":"NOT_FOUND","message":"That could not be found."}}` |

Both are ordinary JSON error responses with 4xx status — no stack trace, no
500, no crash — and the second case specifically exercises the
`isNotFound` branch `AdminMailMessagePage`'s own header comment calls out as
"the whole point" of checking the miss before the generic error gate.

**Result:** PASS.

**Caveats:** None.

---

### 10. Viewing a message twice writes exactly one more `admin_audit_log` row per view, with no URL or body in `detail`

**Done:** React Query's `staleTime` is 30 seconds globally (`main.tsx`), so a
soft, client-side re-visit of the same message inside that window would
serve from cache and fire no second request — the wrong thing to test. Used
full page loads instead (`page.goto`, then `page.reload()`), attached a
`request` listener first, and confirmed each one actually issued
`GET /api/v1/admin/mail/{id}` before checking the database, filtering
`admin_audit_log` by `target like '%/admin/mail/<id>%'` specifically (a bare
row-count would also catch the unrelated `GET /admin/flags` row every full
navigation re-fires for `AdminShell`'s own guard).

**Seen:** Baseline for this message id (from criterion 6's own earlier view)
was 1 row. Two more full loads produced exactly two more rows — 3 total, one
per view, each fired and confirmed on the network layer first:

```
                    action                     |                  target                   | detail
-----------------------------------------------+-------------------------------------------+--------
 GET /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | {}
 GET /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | {}
 GET /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | /api/v1/admin/mail/0X4A0gyWq3bXzedW7NndRq | {}
```

`target` is the request path (which does contain the message id, exactly as
`middleware_admin.go`'s own comment says it will — "the path itself already
contains every value they would have held") but never the message's own
links or body; `detail` is the empty object `{}` in every row, since this
route's URL carries no query string for `auditAdmin` to record. Neither the
extracted link, the plain-text body, nor anything derived from Mailpit's
response appears anywhere in the row.

**Result:** PASS.

**Caveats:** None.

---

### 13. A non-admin household member gets a 404 on `/api/v1/admin/mail`

**Done:** `AdminShell`'s own gate blocks the outlet until `GET
/api/v1/admin/flags` answers, so a non-admin never reaches a mounted
`AdminMailPage` through the UI to fire this request naturally — the request
itself has to be made directly, the same way the households walk used `curl`
for its own guard checks. Requested a fresh magic link for
`jamie@example.test` (an accepted, "limited"-role member of the seeded
household, per an earlier `users`/`memberships` query run at the top of the
walk), consumed it in a fresh cookie-less context to get her real session,
then confirmed her identity — including that she is not a platform admin —
via `GET /api/v1/auth/me` before calling `GET /api/v1/admin/mail` in that
same context. `/auth/me`'s own `isPlatformAdmin` field is the stronger
claim here than a `platform_admins` row count would have been: it is
exactly what `requirePlatformAdmin` itself checks, not a proxy for it.

**Seen:** `/auth/me` confirmed `"isPlatformAdmin": false`, role `"limited"`.
`GET /api/v1/admin/mail` answered:

```json
{"error":{"code":"NOT_FOUND","message":"That endpoint does not exist."}}
```

with HTTP 404 — byte-identical in shape to the router's own catch-all 404,
exactly as `requirePlatformAdmin`'s doc comment in `middleware_admin.go`
promises ("to everyone else the whole surface must look like a typo"). This
route was never given its own guard-test list entry to special-case; it
inherits `requirePlatformAdmin` from the `/admin` group precisely because it
is a plain read under that group, and this check confirms that inheritance
actually holds at runtime, not just in the route table.

**Result:** PASS.

**Caveats:** None.

---

### 14. No horizontal overflow at 305, 360, 768 and 1440 px, on both screens

**Done:** For each width, set the real viewport (`page.setViewportSize`, not
just a CSS media query) and compared
`document.documentElement.scrollWidth` against `window.innerWidth` on both
`/admin/mail` and `/admin/mail/{id}`.

**Seen — first pass, before any fix:** 305px overflowed on **both** screens
(`scrollWidth 319 > innerWidth 305`); 360, 768 and 1440 were clean. Isolated
the cause with `getBoundingClientRect`/`scrollWidth` on the header's own
children rather than guessing: the **operator header's nav**
(`OperatorNav` in `AdminShell.tsx` — shared by every `/admin/*` route, not
code this feature added its own copy of) was the culprit, not the message
body or the long link text advisor-review flagged as the likelier suspect
(that text wraps cleanly on its own, confirmed below). Proved it directly by
toggling the `Mail` link's `display` off in the live page: `scrollWidth`
dropped from 319 to exactly 305 with it hidden. **This nav had only two
links (`Flags`, `Households`) before this task added `Mail`** — adding a
third item is what pushed the shared header 14px past 305px, on every
`/admin/*` page, not only this feature's own two screens. This is treated as
this task's defect to fix, not a pre-existing one to merely note, because
the diff that caused it is this task's diff.

**Fix:** `web/src/features/admin/AdminShell.tsx` — `OperatorNav`'s `<nav>`
gained `flex-wrap` (plus `justify-end` so a wrapped second row still hugs the
right edge, and `gap-x-4 gap-y-1` in place of `gap-4` so the two wrapped rows
sit close together): the nav now wraps onto a second line, on any width
narrow enough to force it to, rather than ever pushing the page wider than
the viewport. At 305px, "Flags · Mail · Households" sits on the header's own
line next to the title, and "Back to Hearth" wraps to a second line,
right-aligned; at 360px and above nothing changes — the fix has zero visual
effect once content fits on one line, confirmed by the header height staying
exactly 48.25px at 1440px, matching its height before this change.

**Seen — after the fix, all sixteen combinations:**

| Width | `/admin/mail` | `/admin/mail/{id}` | `/admin/flags` (sweep) | `/admin/households` (sweep) |
|---|---|---|---|---|
| 305 | 305 = 305 | 305 = 305 | 305 = 305 | 305 = 305 |
| 360 | 360 = 360 | 360 = 360 | 360 = 360 | 360 = 360 |
| 768 | 768 = 768 | 768 = 768 | 768 = 768 | 768 = 768 |
| 1440 | 1440 = 1440 | 1440 = 1440 | 1440 = 1440 | 1440 = 1440 |

No overflow anywhere. The sweep of `/admin/flags` and `/admin/households`
(neither part of this task's own two screens, but sharing the same fixed
`AdminShell`) is the "check whether the same shape exists elsewhere" step —
here the shape was a single shared component, so one fix closed it
everywhere at once rather than needing a second instance found and patched.

On the detail screen specifically, the long invite link (both in the Links
row, which has its own `break-all`, and inline in the plain-text body, which
has only `whitespace-pre-wrap`) was checked separately at 305px and wraps
cleanly without contributing any overflow of its own — the browser breaks
long dash-containing "words" at the internal hyphens
(`http://localhost:5173/invite/6C5huD3Mdl-` /
`nJHXHMpAN4TkfyB3l6uYupa2h_POP81U`), so the `break-words` utility
advisor-review suggested pre-emptively adding was not needed once the
actual, measured cause (the nav) was fixed and re-tested — confirmed by
measurement, not left as an assumption.

**Test added:** `web/src/features/admin/AdminShell.test.tsx` (new file) — one
test renders the shell and asserts the nav's four links appear in the order
`AdminShell.tsx`'s own `items` array defines (Flags, Mail, Households, Back
to Hearth), and a second asserts the `<nav>` carries `flex-wrap`. jsdom runs
no real layout engine, so neither test can assert "no horizontal overflow at
305px" directly — the file's own header comment says so rather than
implying more coverage than it has. **Mutation-checked**: reverted the
`className` to its pre-fix value (`"flex items-center gap-4"`, no
`flex-wrap`) and re-ran the suite — the wrap assertion failed exactly as
expected (`AssertionError: expected 'flex items-center gap-4' to contain
'flex-wrap'`), the order assertion still passed (proving it is not itself
sensitive to this defect, which is correct — it is a different assertion for
a different regression), then the fix was restored and both tests passed
again.

**Result:** PASS (after fix). **A real defect was found and fixed in this
task**, in `AdminShell.tsx`, which this task's own diff (adding the `Mail`
link) caused.

**Caveats:** See `docs/LEARNING.md` for the recorded lesson, and Step 4's
`make lint && make test` run at the end of this file for the regression
suite this fix was checked against.

---

### 15. The browser console carries no error on either screen

**Done:** Fresh full-page loads of `/admin/mail` and
`/admin/mail/0X4A0gyWq3bXzedW7NndRq` (as the signed-in admin, after criteria
9's deliberate negative-path checks had already generated their own
expected 400/404 console noise on earlier, unrelated navigations — checked
each screen's console immediately after its own fresh navigation so that
noise is not what gets counted).

**Seen:** Both loads: `Total messages: 3 (Errors: 0, Warnings: 0)` — the
three are Vite's own `[vite] connecting…` / `[vite] connected.` dev-client
messages and the React DevTools suggestion banner, none of them errors or
warnings, and none originating from this feature's own code.

**Result:** PASS.

**Caveats:** None.

---

### 11. Mailpit unreachable

**Done:** `docker compose stop mailpit`, then reloaded `/admin/mail`.

**Seen:** The page rendered "Mailpit is not answering. The messages are not
lost — the reader is." — an inline alert, not a crash — and
`GET /api/v1/admin/mail` answered `502` with
`{"error":{"code":"MAIL_UPSTREAM_UNAVAILABLE", ...}}`. Checked the full page
text specifically for the string `MAILPIT_API_URL`: absent. This is the
right copy for this failure — the variable is set correctly; the container
behind it is simply down — and confusing the two would send an operator to
edit configuration that was never the problem.

Restarted Mailpit afterward (`docker compose start mailpit`) and confirmed
its own API answers again. Checking its message store immediately after
confirmed it came back **empty** (`"total": 0`) — Mailpit's dev container
carries no volume, so a stop/start (not just an unreachable blip) cleared
every message this walk had generated, exactly what the page's own
durability line warns about. This is the line proving itself true, not a
defect: recorded here rather than treated as one. No later criterion still
needed a message present to pass (10, 14 and 15 had already run against
real content while it existed; criterion 12 only needs the API's own
configured/unconfigured behaviour, not Mailpit's contents), so none was
regenerated — the walk finished with Mailpit's store empty (see "State
changed on the dev box" below).

**Result:** PASS.

**Caveats:** None.

---

### 12. Not configured

**Done:** Copied `docker-compose.yml` to a scratch file first
(`cp docker-compose.yml <scratchpad>/docker-compose.yml.orig`) so the
restore step could be a plain file copy rather than a git operation.
Commented out the `MAILPIT_API_URL:` line (`api` service, line 41), ran
`docker compose up -d api` (which recreates the container with the new
environment — a plain restart would not pick up a changed `environment:`
block), polled `GET /healthz` on the API until it answered `200` (the
container needs a moment to rebuild its dependency graph on start), then
reloaded `/admin/mail`.

**Seen:** The page rendered "The message inspector is not configured on this
install. Set MAILPIT_API_URL and restart the API." — naming the exact
variable — and `GET /api/v1/admin/mail` answered `503` with
`{"error":{"code":"MAIL_INSPECTOR_NOT_CONFIGURED", ...}}`.

**Restore:** Copied the scratch file back over `docker-compose.yml`
(`cp <scratchpad>/docker-compose.yml.orig docker-compose.yml`), confirmed
`git diff --exit-code docker-compose.yml` reported no difference (exit 0)
and that its MD5 matched the pre-edit copy exactly, then ran
`docker compose up -d api` again and polled `/healthz` until it answered
`200`. Reloaded `/admin/mail` once more: back to normal (`"No mail sent
yet."` — Mailpit's store had already been cleared by criterion 11's
stop/start, so this is the expected empty state after two separate real
outages, not a new one).

**Result:** PASS.

**Caveats:** `docker-compose.yml` is confirmed byte-identical to the
committed version (see the note under "Final state" below with the actual
`git diff` output at the end of this walk).

---

## Beyond the criteria

**A. The copy button's silent no-op on a non-secure context was not directly
testable here, and is worth naming anyway.** `LinkRow`'s `handleCopy` in
`AdminMailPage.tsx` returns immediately, with no visible change at all, if
`navigator.clipboard` is `undefined` — which happens on plain HTTP in a real
browser (this dev box's `http://localhost` is treated as a secure context by
Chrome regardless, so the guard never fired during this walk). An operator
on a genuinely non-HTTPS install would click "Copy link," see nothing
happen, and have no way to tell the click failed from the click succeeding
silently with an empty clipboard. Not a defect against any of the fifteen
criteria — the dev environment cannot reach this branch to prove it either
way — but worth a follow-up: at minimum, the button should say "Can't copy"
rather than doing nothing.

**B. The dev web container's file watcher did not pick up a source edit
without a container restart.** While building the fix for criterion 14,
saving `AdminShell.tsx` produced no Vite HMR log line and the dev server
kept serving the pre-edit compiled module (confirmed by fetching
`/src/features/admin/AdminShell.tsx` directly and finding the old
class name still there, several seconds after the file on disk — and inside
the container — already had the new one). `docker compose restart web`
fixed it, at the cost of a fresh `npm install` on restart (the container's
entrypoint is `npm install && npm run dev`), which took about 80 seconds.
This is infra behavior, not a product defect, but it is worth writing down
so a future change to this same shared file does not get diagnosed as "my
fix didn't work" when it is "the dev server didn't notice the file changed."

**C. Claude in Chrome's screenshot capture on this box silently drops all
but the first item of a flex row, at an unusual `devicePixelRatio: 2.5`.**
Documented in full at the top of this file, ahead of the criteria — recorded
here too because it is the kind of failure that looks exactly like a real
"invisible nav link" defect (criterion 2's own history) until cross-checked
in a second tool. Anyone re-running this walk on the same or a similarly
scaled display should expect the same false signal and know to reach for
Playwright (or a real, unscaled display) before concluding the product is
broken.

---

## State changed on the dev box

- **`andreas@hearth.family`'s password is now `hearth-dev-password`** (the
  documented development default), reset via
  `make reset-password EMAIL=andreas@hearth.family` before the walk could
  safely proceed — see the environment note at the top of this file. This
  also revoked every session that existed for that account at the time.
- **A new pending invite exists**: `walk-2026-09-04@example.test`, role
  Parent/owner, created from Settings, not accepted (expires
  2026-09-11). Left in place rather than cancelled — the invite screen has
  no cancel action to run, and an unaccepted invite is inert.
- **`jamie@example.test` (an existing "limited" household member) signed in
  once**, via a magic link requested and consumed solely to prove criterion
  13 — a completely ordinary sign-in indistinguishable from her using the
  product; her session will expire naturally like any other.
- **`admin_audit_log` grew from a pre-walk count to 143 rows** — expected:
  the table is append-only, and this walk deliberately generated many reads
  under `/admin` (including the 32 extra navigations criterion 14's overflow
  sweep alone produced across four widths, two screens, and two sibling
  pages).
- **`login_attempts` holds 20 rows**, including the one genuine failure at
  the very start of the walk (the stale-password discovery) and the
  magic-link-based sign-ins for `andreas@hearth.family` and
  `jamie@example.test`.
- **`sessions` holds 24 rows** — normal accumulation from this walk's own
  sign-ins (each fresh incognito-equivalent context for criteria 7 and 13
  created one) plus whatever the box already carried; none were deleted.
- **Mailpit's message store is currently empty.** Criterion 11's stop/start
  cleared it (Mailpit's dev container has no volume), and no later criterion
  needed content back — criterion 12 restarts only the `api` container, not
  Mailpit, so the store stayed empty from criterion 11 onward. This is the
  durability line's own claim, proven rather than merely stated. Nobody
  after this walk should expect to find `walk-2026-09-04@example.test`'s
  invite email sitting in Mailpit; the invite itself is still live in
  Postgres (see above), only its email copy is gone.
- **`docker-compose.yml` is confirmed unchanged** — see Step 4 below for the
  `git diff --exit-code` run at the very end of this walk, after all other
  work was committed, as a final guard against having left it edited.
- **`web/src/features/admin/AdminShell.tsx` changed for real**: the
  `flex-wrap` fix for criterion 14. This is the one intentional, permanent
  code change this walk made, and it is committed with this file.
- **`web/src/features/admin/AdminShell.test.tsx` is a new file**: the
  mutation-checked regression test for that fix (see criterion 14).
- Both the `api` and `web` containers were recreated/restarted during this
  walk (api twice, for criterion 12's before/after; web once, to pick up the
  `AdminShell.tsx` fix) — both came back healthy and are running normally as
  of the final `make lint && make test` run below.

---

## Step 4: final full run

```
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

**`make lint`**: architecture lint passed; `tsc --noEmit` clean; `eslint .`
clean; `go vet ./...` clean.

**`make test`**:

- Go suite (`go test ./... -count=1 -timeout=5m`, testcontainers against the
  colima Docker socket): every package `ok`, including
  `internal/adapter/http` (216.7s) and `internal/adapter/postgres` (242.3s),
  the two that carry the outbound-mail route and repository tests. Timings
  are from the run before `AdminShell.test.tsx` existed — a frontend-only
  file, so this is unaffected by it, not re-run afterward.
- Frontend (`npx vitest run`), run again after adding the new file:
  **78 test files, 756 tests, all passed** — 77/754 immediately before.

Both green. `docker-compose.yml` confirmed unchanged one more time,
immediately before this section was written:

```
$ git diff --exit-code docker-compose.yml
$ echo $?
0
```

---
