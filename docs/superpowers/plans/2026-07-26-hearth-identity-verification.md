# Hearth identity slice — definition-of-done walkthrough (Task 21)

Date: 2026-07-27
Environment: colima docker (`DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`), real Chromium via Playwright MCP, `http://localhost:5173`.

## Setup

```
$ make down
docker compose down
$ docker volume rm hearth_hearth-pgdata
hearth_hearth-pgdata
$ make up
... (built and started postgres, mailpit, api, web; all healthy)
$ make seed
Seeded the household "Andreas & Christine".
  Andreas:            andreas@hearth.family / hearth-dev-password
  Christine's invite: http://localhost:5173/invite/hearth-dev-invite-token
```

**Precondition noted before the walkthrough started:** reading `api/internal/usecase/seed.go` shows Christine's seeded invite carries `role: owner` with `domain.AllCapabilities()` (`issueChristineInviteAtNextRung`, seed.go:404-423). Kayla is seeded `RoleLimited` with `{calendar, chores}`; Ethan `RoleLimited` with `{calendar}`; Andreas `RoleOwner` with all four capabilities and a real password hash (`DevPassword = "hearth-dev-password"`).

This means that **once Christine accepts her invite in criterion 1, the household has two owners (Andreas and Christine), not one.** Criterion 9 as literally worded ("while he is the only owner") has a precondition that criterion 1 itself destroys. This is recorded as a finding under criterion 9 below, along with what was actually observed both as literally scripted and under a supplementary condition that restores the "only owner" precondition.

---

## Criteria

### 1. Accept Christine's invite

Steps: navigated to `http://localhost:5173/invite/hearth-dev-invite-token`. Page rendered "Andreas invited you in.", "You'll share Money, Marriage and Family.", "Joining as **co-owner** — full access, equal say on every agreement.", a pre-filled Name field ("Christine"), a Password field with helper text "At least 12 characters.", and "Accept & join household". Filled password `christine-secure-pass` (21 chars) and clicked Accept & join household.

Observed: navigated to `/` and rendered the signed-in shell immediately — sidebar with Overview / Money / Marriage / Family / Settings, avatar initial "C", household name "Andreas & Christine", "Sign out" button. No error banner. Two console messages present (401 on `/api/v1/auth/me` before the invite was accepted, 404 on `/favicon.ico`), both benign and unrelated to the invite flow.

**PASS.**

### 2. Sign out, sign in as Christine

Steps: clicked "Sign out" (⏻). Redirected to `/sign-in`, rendering "Welcome back.", Email field, Password field with "Forgot?" link, "Continue", and "Email me a one-time sign-in link". Filled Email = `christine@hearth.family`, Password = `christine-secure-pass`, clicked Continue.

Observed: navigated to `/` and rendered the signed-in shell again — avatar "C", "Andreas & Christine", full sidebar. Sign-out and password sign-in both round-tripped correctly.

**PASS.**

### 5. Sidebar spaces and placeholder destinations (checked here, as Christine/owner, before locking the household)

Steps: while signed in as Christine (owner, all capabilities), read the sidebar, then clicked Money, Marriage, Family in turn.

Observed: sidebar shows exactly Overview, Money, Marriage, Family, Settings, in that order — matches `domain.BuiltinSpaces` order (money, marriage, family) plus the two app-nav entries Overview and Settings.
- `/money` → heading "Money", "Arriving in slice 2."
- `/marriage` → heading "Marriage", "Arriving in slice 3."
- `/family/calendar` → heading "Family calendar", "Arriving in slice 4."

All three placeholder destinations rendered with no error banner and no console error beyond the benign 401/404 already seen.

**Supplementary check — capability-filtered sidebar for a real limited member.** Kayla and Ethan (the seeded children) have no email or password (`usecase/seed.go`'s `ensureChild` invites them with an empty email), so neither can actually sign in and there is no way to observe their own sidebar directly. To still verify "the sidebar shows the spaces the server sent" for a capability-restricted account rather than just an owner, I invited a new limited member with an email ("Tessa", role Kid, Calendar only — Chores and Money both left off) from Settings, accepted her invite in a separate tab (which correctly warned "Signed in as Andreas. Accepting this invite will sign them out and sign you in instead." before doing it), and read her sidebar. Result: exactly **Overview, Family, Settings** — no Money, no Marriage. This matches `domain.VisibleSpaces`: Money requires the `money` capability (absent), Marriage is `parents_only` (Tessa is `limited`), Family has no required capability. The filtering is real and server-driven, not just decorative in the owner case.

**PASS** for both the owner view and the capability-filtered view.

### 3. Lockout: three wrong passwords against Andreas's email

Steps: signed out of Christine's session, landed on `/sign-in`. Submitted `andreas@hearth.family` with a wrong password four times in a row (changing the password string each time so nothing was cached), reading the inline alert after each submit.

Observed, verbatim:
1. 1st wrong attempt → `That password doesn't match. Two tries left before we lock the household for 15 minutes.`
2. 2nd wrong attempt → `That password doesn't match. One try left before we lock the household for 15 minutes.`
3. 3rd wrong attempt → `This household is locked for 15 minutes after too many failed attempts. Use a magic link below to sign in instead.` The Continue button became `disabled`, and pressing Enter in the password field did not fire a request (console error count unchanged) — the frontend actively prevents a UI-driven 4th submit once locked.
4. To still exercise a literal 4th attempt (since the UI blocks it), called `POST /api/v1/auth/sign-in` directly from the page's own origin with the session's cookies via `fetch`. Response: `HTTP 423`, body `{"error":{"code":"HOUSEHOLD_LOCKED","message":"This household is temporarily locked after too many failed sign-in attempts.","details":{"lockedUntil":"2026-07-26T19:12:26.495089Z"}}}`.

**Finding — the brief's numbering is off by one from what the system actually does.** The brief states "the **second** attempt reads `Two tries left`, the **third** reads `One try left`, and the **fourth** returns the locked message" — implying 4 attempts are needed to lock. What was observed is `domain.DefaultLockoutPolicy` behaving exactly as its own unit tests specify (3 max attempts, and `AttemptsRemaining` is computed *after* recording the just-failed attempt): the **1st** wrong attempt already reads "Two tries left" (2 remaining after 1 failure), the **2nd** reads "One try left" (1 remaining after 2 failures), and the **3rd** wrong attempt is what locks the household. A 4th attempt is not needed to reach the locked state — it only confirms the lock holds. This is a real behavior, not a bug: `TestOneFailureLeavesTwoTries` in `api/internal/domain/lockout_test.go` pins exactly this. The mismatch is between the brief's prose and the shipped, tested, intentional behavior.

Judged against what the criterion is actually checking — that the three specific messages appear in order and that the household ends up locked and confirmed locked on a further attempt — all of that is true, just one attempt earlier than the brief's count. **PASS**, with the off-by-one documented above so it isn't silently "corrected" into the record.

### 4. While locked, magic link signs in

Steps: while still on the locked sign-in screen with `andreas@hearth.family` in the Email field, clicked "Email me a one-time sign-in link". Opened Mailpit (`http://localhost:8025`), found the new "Your Hearth sign-in link" email addressed to `andreas@hearth.family`, opened it, copied the full link text (`http://localhost:5173/sign-in/magic?token=-sM2oRkyGeYIBMiegMod-yAQTlqCyT90iVU7XdA8LpY`), and navigated the browser to it.

Observed:
- Requesting the magic link succeeded while locked (`POST /api/v1/auth/magic-link` — the design intent that "magic link is never gated by the lock", per `usecase/auth.go` and the plan's Global Constraints, held).
- The email arrived in Mailpit immediately, addressed correctly, with a working, complete link and 15-minute / single-use copy.
- Following the link rendered `MagicLinkConsumeScreen`: "Signing you in…". `browser_network_requests` showed `POST /api/v1/auth/magic-link/consume => 200 OK`, and its response body was a fully-formed `Me` object for Andreas (owner, all four capabilities). A direct `fetch('/api/v1/auth/me')` from the page confirmed the session cookie was live and the server considered Andreas signed in.
- **The page itself never left "Signing you in…".** No navigation occurred, no error banner appeared, and `browser_console_messages` showed zero console errors (down to debug level) after the consume request completed. Waited 5+ seconds with no change. Manually navigating to `/` in the same browser session immediately rendered the full signed-in shell as Andreas (avatar "A") — proving the session was valid the whole time and only the client-side redirect was missing.
- **Reproduced independently, cleanly, a second time** with a fresh magic-link request/token (`...token=g7mOXyYipMaDaUlklbhoO98R-pJFn5zDs6Q0btsmEmM`) to rule out interference from my own tooling: signed out, requested a new link, opened it in Mailpit, navigated directly to the link with no intervening calls, waited 4 seconds untouched. Same result — `POST /auth/magic-link/consume => 200 OK`, stuck on "Signing you in…", zero console errors, session valid underneath.

**Mechanism: not fully determined, and I'm not overstating it.** `web/src/features/auth/MagicLinkConsumeScreen.tsx` calls `consume.mutate({ token }, { onSuccess: () => navigate({ to: "/", replace: true }) })` once per mount (guarded against StrictMode's double-invoke by a ref). The consume request itself completed in 6ms per its own network-request timing, and the session was valid immediately after, so the mutation did resolve successfully. What I did **not** instrument is whether `onSuccess` ran and called `navigate(...)` which then failed to commit, or whether `onSuccess` itself never ran. Both are consistent with everything observed (zero console output either way): a `navigate()` call issued while the router was still finishing its own initial load could plausibly be dropped, and a callback that silently never fires is equally possible given no logging exists on this path. This is not a backend defect either way — `usecase/auth.go`'s `ConsumeMagicLink` and the session cookie plumbing are confirmed correct by the successful `/auth/me` call. The user-facing failure (stuck on "Signing you in…" with no recourse) is established regardless of which of the two it is.

**FAIL**, on the criterion's own terms ("open the link... and confirm it signs you in"): a real user clicking this link is left staring at "Signing you in…" indefinitely, even though they are in fact signed in underneath. Recovering requires knowing to reload or navigate manually, which nothing on the screen suggests. Working around this (via a manual navigation to `/`) was necessary to reach the account as Andreas for the remaining criteria, and is recorded here as the deviation the brief asks for when a step must be forced to continue.

**Why `make test` (run later, see below) does not catch this:** `MagicLinkConsumeScreen.test.tsx` has exactly two tests — that the consume request fires once under StrictMode, and that an error message renders on a 410. Neither asserts that a *successful* consume actually navigates anywhere. Worse, `renderWithRouter` (the shared test harness) mounts the component as a single root route with no other routes registered, so even a test that tried to assert "navigated to `/`" would be checking a router that structurally has nowhere else to go — it cannot distinguish "navigate fired and worked" from "navigate never fired" the way the app's real, multi-route tree can. This is the same class of gap the plan's own history flags for Task 19 (jsdom's `HTMLDialogElement` lacking `showModal` let a modal that threw in every real browser pass all five of its tests): a real, reachable, user-facing failure with 100% green unit tests over it.

### 6. Settings: change primary currency and toggle every notification; reload; confirm persisted

Steps: navigated to `/settings` as Andreas. The Settings page rendered Members, Spaces, Currency & region, and Notifications cards.

**Finding — there is no UI control to change the primary currency.** `web/src/features/settings/CurrencyPanel.tsx` (lines 1-4, 66-72, 102-113) renders the primary-currency row and the FX-rate row as plain, non-interactive text by deliberate design — its own comment says so, and it mirrors the design mockup exactly: `design/Household Dashboard.dc.html` line 714 has no `onClick`/`cursor:pointer` on the primary-currency span either (unlike the secondary-currency toggle on the next line, which does). `useUpdateHousehold`'s mutation payload type is `{ showSecondaryCurrency: boolean }` only — there is no code path in the shipped UI that can PATCH `primaryCurrency`. Clicking directly on the "SGD (S$)" text produced no dropdown, no modal, and no state change.

To characterize whether this is only a missing UI control or a deeper problem, I called `PATCH /api/v1/household` directly (`{"primaryCurrency":"USD"}`) from the page's own origin with its live session/CSRF cookies. It answered `200` with the updated household (`"primaryCurrency":"USD"`), and reloading `/settings` showed the read-only field update to "USD ($)" — so the backend and persistence are sound; **only the Settings screen exposes no way to reach that endpoint for primary currency.** Reverted it back to `SGD` afterward via the same direct PATCH so it doesn't corrupt later criteria.

**Notifications and the one currency toggle that does exist**, done through the real UI: clicked all four notification switches (Bill due reminders, Budget over-spend alerts, Monthly retro reminder, Weekly family digest) — all were `checked` and all read `unchecked` after their click — and clicked "Show secondary currency equivalents" (`checked` → unchecked). Reloaded `/settings` (full page navigation). All five switches were still unchecked after reload: toggling and persistence work correctly for everything the UI actually exposes.

**FAIL.** The criterion is a single conjunction ("change the primary currency **and** toggle every notification ... confirm both persisted"), and the first half cannot be performed at all — no control exists in the shipped Settings screen for it, let alone one whose persistence could be verified. The notification half is unambiguously good (see above), but a criterion that requires both is not satisfied by half of it, so the criterion as a whole is graded **FAIL**, not split. (The notification-toggle behavior itself is correct and is credited as such in the Summary below.)

### 7. Settings: invite a new member; confirm email arrives in Mailpit

Steps: clicked "+ Invite" in the Members card. A modal opened ("Invite a family member") with no console error (real Chromium — this is the exact modal Task 19's fix targeted). Selected Role = "Parent" (the disabled, all-checked capability switches — Calendar, Chores & allowance, Money & balances, Marriage — updated correctly to reflect "an owner holds every capability"). Filled Name = "Priya", Email = "priya@hearth.family", clicked "Send invite".

Observed: the modal closed with no error banner. Opened Mailpit and found a new email: "Andreas invited you to Hearth", To: `priya@hearth.family`, with a working `/invite/<token>` link.

**PASS.**

### 8. Settings: remove Kayla's `chores` capability; confirm persists after reload

Steps: on the Settings page (still signed in as Andreas), found Kayla's row ("Kid · calendar & chores only") and clicked her "Chores" switch (checked → unchecked). Reloaded `/settings` (full page navigation).

Observed: before the click, Kayla's row read "Kid · calendar & chores only" with Calendar and Chores both checked, Money unchecked. After the click, the Chores switch read unchecked immediately. After reload, Kayla's row read "Kid · calendar only" and the Chores switch was still unchecked — the change persisted server-side, not just in local component state.

**PASS.**

### 9. Attempt to remove or demote Andreas while he is the only owner; confirm `LAST_OWNER`

**The precondition does not hold after criterion 1, as flagged at the top of this document.** Christine's seeded invite is `role: owner` with all four capabilities (`usecase/seed.go`'s `issueChristineInviteAtNextRung`), so once she accepted, Andreas was never again the household's only owner — Christine is a second one, for the entire rest of this walkthrough, unless and until someone demotes or removes her. I tested both the literal scenario (as scripted) and the scenario the criterion actually intends (a genuine sole owner), and both are recorded below.

**Finding — there is no "remove a member" control anywhere in the shipped UI.** Reading `web/src/features/settings/MembersPanel.tsx` end to end: the only member-editing controls are a per-member role toggle (`toggleRole`, Owner ↔ Limited) and, for limited members, three capability toggles. There is no delete/remove button, icon, or menu item for any member. Checked both ends of this: `api/internal/adapter/http/router.go:94` registers `o.Delete("/household/members/{id}", handleRemoveMember(deps))` in the owner-only group, so the backend can remove a member — but the design's own Members mockup (`design/Household Dashboard.dc.html:696-699`) has no removal affordance either, just a static Owner/Limited badge per row with no `onClick` at all. So this isn't a UI gap against an explicit design intent — the design itself never depicted removal — but the criterion still cannot be performed as "remove" through the UI, only as "demote" (toggle the role switch), which the criterion's own wording also accepts ("remove or demote").

**Step A — literal criterion, using the role toggle, with Christine still an owner:** signed in as Andreas, on `/settings`, clicked Andreas's own role switch (Owner → Limited). Observed: `PATCH /api/v1/household/members/<andreas-membership-id>` returned `200 OK` with `{"capabilities":["calendar","chores","money"],"id":"...","role":"limited"}` — the demotion **succeeded**, not blocked by `LAST_OWNER`, because Christine remained an owner. Andreas's own session was then immediately revoked server-side (`usecase/member.go`'s `RevokeAllForUser`, called after any successful membership update — by design, not a bug: it forces re-authentication so a changed role/capability set can't keep acting under a stale session) and the browser was redirected to `/sign-in`. **This is the expected, correct behavior of `domain.ValidateMembershipChange` given the actual household state — it is not a defect, but it does mean the criterion's literal premise is false and its literal execution does not exercise `LAST_OWNER` at all.**

**Step B — restoring the sole-owner precondition, then re-attempting:** signed back in as Christine (magic link, since the household was still inside its criterion-3 password lock window). On `/settings`, Christine was now the household's only owner (Andreas had just been demoted in Step A). Clicked Christine's own role switch (Owner → Limited). Observed: `PATCH /api/v1/household/members/<christine-membership-id>` returned **`409 Conflict`**, body exactly `{"error":{"code":"LAST_OWNER","message":"A household must keep at least one owner."}}`. The UI rendered the alert **"A household must keep at least one owner."** directly under Christine's row, and her role switch remained `checked`/"Owner" — nothing changed. This is the criterion's actual intent, verified.

Restored Andreas to Owner afterward (clicked his role switch again) so the household ends the walkthrough in its original two-owner state.

**FAIL, on the criterion as written.** It specifies a precondition ("while he is the only owner") and a specific target (Andreas) that this walkthrough cannot jointly satisfy: the precondition is false by the time this step is reached, purely as a consequence of criterion 1, and "remove" has no UI path regardless of who the target is. Grading this PASS on the strength of the underlying rule would blur the distinction this report is supposed to preserve — the rule being correct (see below) and the criterion being executable as specified are two different questions, and here the second one is genuinely no. The `LAST_OWNER` rule itself is correctly implemented and enforced — confirmed directly with an exact 409/`LAST_OWNER` response and an unchanged UI when tried against a genuine sole owner (Step B) — and that fact is credited in the Summary as a thing that demonstrably works, separately from this criterion's own pass/fail.

### 10. Sign out; confirm `/` redirects to `/sign-in`

Steps: clicked "Sign out" from `/settings` (as Christine). Then, in a separate navigation, requested `http://localhost:5173/` directly.

Observed: after sign-out, landed on `/sign-in`. Navigating straight to `/` afterward also rendered `/sign-in` ("Welcome back.", Email/Password fields, "Email me a one-time sign-in link") — the router redirected the protected root route to sign-in rather than rendering the app shell or any protected content.

**PASS.**

---

## Fixes and re-walk

Everything above is the original walkthrough, left exactly as observed — nothing in it was rewritten after the fixes below. This section records what changed and re-verifies the three criteria that failed, against a second, independent clean start (`make down`, volume removed, `make up`, `make seed`).

### Fix 1 (Critical) — magic-link consume screen never navigates

**Determining the mechanism.** Instrumented `MagicLinkConsumeScreen.tsx` and `useConsumeMagicLink` (`useAuth.ts`) with `console.debug` at every step — mutate() called, mutationFn start/resolve, the mutation's own `onSuccess`, the per-call `onSuccess`/`onError`/`onSettled` passed to `.mutate()`, and the component's own render/mount/unmount lifecycle — then reproduced the bug again with the instrumented build. The trace showed, every time: `mutationFn resolved` → the mutation's own `onSuccess` fired → **nothing after that**. No per-call `onSuccess`, no re-render, no console error. One log line stood out: `lifecycle effect UNMOUNTING` appeared immediately after `mutate() called`, then the effect fired its setup again — the exact shape of dev-mode React StrictMode's double-invoke of every effect once, synchronously, right after the initial mount.

Reading `@tanstack/query-core`'s own source (`node_modules/@tanstack/query-core/build/modern/mutationObserver.js`, `#notify()`) confirmed why: the callbacks passed as the second argument to `.mutate(variables, options)` are only invoked `if (this.#mutateOptions && this.hasListeners())` — i.e. only while some component is still actively subscribed to that specific `MutationObserver` instance. `Mutation#execute()` (`mutation.js`), by contrast, `await`s the mutation's *own* `onSuccess`/`onError` (the ones passed to `useMutation({...})` itself) **unconditionally**, with no such gate. `MagicLinkConsumeScreen` fired its mutate() call from its own mount effect — exactly the moment StrictMode tears down and rebuilds every effect in the tree, including this hook's own subscription (`useSyncExternalStore` inside `useMutation`).

Comparing against `AcceptInviteForm` (`InviteScreen.tsx`), which uses the identical per-call-`mutate()`-onSuccess pattern and was already confirmed working (criterion 1): the difference is that its `mutate()` call fires from a **user-triggered form submit**, long after StrictMode's one-time mount dance has already finished — not from a mount effect. Both call sites use the same TanStack Query API; only one of them fires it from the exact synchronous window that dance runs in.

I could not fully instrument *why* `hasListeners()` ends up false at that instant (that lives inside React/TanStack internals I didn't reach for). Worth being explicit about a gap in this theory: StrictMode's mount-effect teardown-and-rebuild is fully synchronous — it unsubscribes and resubscribes back-to-back, before any awaited fetch can settle — so it doesn't cleanly explain a subscription still being absent later, when the mutation actually resolves. It remains the leading candidate (it's the only thing in the trace that touches this subscription at all, and it matches the `lifecycle effect UNMOUNTING` line seen at exactly this moment), but I'm recording it as the leading candidate, not a confirmed cause. What *is* confirmed, and what the fix actually rests on, is the asymmetry itself: per-call callbacks are gated on `hasListeners()`, hook-level ones are not, and this call site (mount effect) versus the working one (`AcceptInviteForm`'s form submit) differ in exactly the way that asymmetry predicts.

**The fix.** `useConsumeMagicLink` (`web/src/features/auth/useAuth.ts`) now attaches `onSuccess`/`onError` to the mutation's own options (reliable) instead of `MagicLinkConsumeScreen` passing them to `.mutate()` (unreliable from a mount effect). `onSuccess` now also calls `queryClient.setQueryData(meQueryKey, data)` to prime the `['me']` cache directly and synchronously from the mutation's own response (`invalidateQueries` alone was never enough here: it only forces a refetch of *active* observers, and nothing on `/sign-in/magic` mounts a `useMe()` of its own), then `navigate({ to: "/", replace: true })`. `onError` sets a local `errorMessage` (plain `useState`, not react-query's own listener-gated `consume.isError`/`consume.error` — the exact same gate that broke `onSuccess` would equally have broken relying on a re-render to show the error message, so both outcomes were moved off it symmetrically). `MagicLinkConsumeScreen.tsx` is simplified accordingly: it just calls `mutate({ token })` once and renders whichever of "Signing you in…" or `errorMessage` the hook reports.

**Tests added.**
- `web/src/features/auth/mutationObserverCallbacks.test.ts` (new): a direct, deterministic unit test against `MutationObserver` itself — subscribe, call `.mutate(vars, { onSuccess })`, unsubscribe before the mutationFn's promise resolves (standing in for whatever drops the subscription in production), and assert the per-call `onSuccess` is *not* invoked while the mutation's own `onSuccess` *is*; a second case shows the per-call one firing fine when the subscription is never dropped. This does not depend on React/jsdom timing, but it is not, on its own, a regression guard for this app-level bug either: it tests query-core's behaviour, not this codebase's — someone who reintroduces `mutate(vars, { onSuccess })` from a mount effect elsewhere would leave it green. What it guards is the library invariant the fix's reasoning depends on, so that reasoning stays correct even if a future TanStack Query upgrade changes the internals. The actual defence against the anti-pattern recurring is the real-browser walkthrough below.
- `web/src/features/auth/MagicLinkConsumeScreen.test.tsx`: added a test asserting the screen navigates to `/` on success. **Documented honestly, not oversold:** I confirmed empirically (reverted the fix via `git stash`, re-ran this exact test) that it **passes against the old, buggy code too** — jsdom does not reproduce the StrictMode/timing conditions this bug depended on, the same class of gap the plan's history records for Task 19's Modal (jsdom's `HTMLDialogElement` has no `showModal` at all, so a real-browser-only crash passed every test). It's kept because it correctly documents the intended behavior, not because it would have caught the regression — that's what the `mutationObserverCallbacks.test.ts` test is for.

**Re-walk, real browser, second independent clean start (`make down` → volume removed → `make up` → `make seed`):** signed in as Andreas, submitted three wrong passwords to lock the household (verbatim messages matched criterion 3 exactly again), clicked "Email me a one-time sign-in link", opened the email in Mailpit, and navigated to the link. **The browser's own address bar changed from `/sign-in/magic?token=...` to `/` with no further action from me**, landing on the signed-in app shell as Andreas (avatar "A", full sidebar) — no error, no console output beyond the benign vite/devtools lines. **Repeated a second time, independently, with a fresh magic-link request and a different token, from a full sign-out** — identical result. This is the same "request while locked → follow from Mailpit → land signed in without touching the address bar" scenario the original bug was found in, now passing both times.

**PASS** (re-walked).

### Fix 2 (Important) — Settings had no control to change the primary currency

**The fix.** `web/src/features/settings/CurrencyPanel.tsx`: for an owner, the primary-currency row is now a text input (pre-filled with the current code, auto-uppercased, capped at 3 characters) plus a "Save" button, inside a `<form>`. Save is disabled unless the input is exactly three uppercase letters (`/^[A-Z]{3}$/`, mirroring `domain.NewMoney`'s own rule) *and* differs from the currently saved value *and* nothing is already in flight. Submitting issues `PATCH /household` with `{ primaryCurrency }`. A rejected save's error is surfaced inline via `apiErrorMessage` (the same helper `MembersPanel`/`InviteScreen`/`NewSpaceModal` already use), which extracts the backend's own message (e.g. `"That currency code is not valid."` for `422 INVALID_CURRENCY`) rather than a generic string. The input keeps the owner's attempted value on a rejected save rather than reverting it, so they can see and correct what they typed. A non-owner still sees the exact original read-only display — no behavior changed for that viewer.

**A finding surfaced while re-walking this: the backend's own currency validation is format-only, not a real-currency check.** `domain.NewMoney` (`api/internal/domain/money.go`) only requires exactly three uppercase letters — it does not check against any actual ISO-4217 list. Submitting `ZZZ` through the new control **succeeded** (`200 OK`, `primaryCurrency: "ZZZ"` persisted) — this is pre-existing backend behavior (from the identity plan's Task 6/9), not something this fix introduced, but it does mean the specific `422 INVALID_CURRENCY` path is unreachable through the UI for any 3-uppercase-letter string, only for something that fails the length/case rule (confirmed directly: a raw `PATCH /household` with `{"primaryCurrency":"sg"}` returned exactly `422` / `{"code":"INVALID_CURRENCY","message":"That currency code is not valid."}`) — which the client-side gate now blocks before it can ever be submitted through the Save button. The inline-error-surfacing behavior itself is still verified, just at the unit-test level (`CurrencyPanel.test.tsx`, stubbing a `422` response directly) rather than through the live UI, since the UI's own validation and the backend's now agree exactly and there is no live-reachable input that exercises the mismatch.

**Tests added** (`web/src/features/settings/CurrencyPanel.test.tsx`): an owner editing the currency and the resulting `PATCH` body; Save staying disabled until the input is exactly three letters and different from the saved value; a stubbed `422` response rendering the backend's own message inline and leaving the owner's rejected input on screen; the existing "renders … for a non-owner" test updated to assert the input is entirely absent for a non-owner (`queryByLabelText` returns null) rather than merely that the read-only text renders.

**Re-walk, real browser, same clean environment as Fix 1's re-walk:** as Andreas (owner) on `/settings`, the Primary currency row showed an input pre-filled `SGD` and a disabled Save button. Typed `usd` — the field displayed `USD` (auto-uppercased) and Save enabled. Clicked Save: `PATCH /household` fired with `{"primaryCurrency":"USD"}`, `200 OK`, Save returned to disabled. Reloaded `/settings`: input still showed `USD` — persisted. Typed `zzz` and saved: backend accepted it (`200 OK`, per the finding above) — reverted to `sgd` afterward via the same control to leave a sensible household state. Confirmed the true `422` path directly against the backend (not reachable through the gated UI): `PATCH /household` with `{"primaryCurrency":"sg"}` → `422` / `"That currency code is not valid."`, matching what the unit test asserts the UI does with that response.

**PASS** (re-walked) for "change the primary currency … confirm persisted." Notification toggling was already verified working in the original walkthrough and needed no re-walk.

### Criterion 9 — re-walked with the amended wording

No code change was needed here — the `LAST_OWNER` rule was already confirmed correct in the original walkthrough (Step B, against a genuine sole owner). Recorded here **by substance**, as instructed: the rule is what's being verified, and the original criterion's precondition was unexecutable as worded (Christine's seed also grants her `owner`), which is a defect in the criterion's wording, not the product.

The second clean start happened to make this straightforward to re-check literally: Christine's invite was not accepted in this pass, so **Andreas genuinely was the household's only owner** at this point, exactly as the criterion's original wording assumes. On `/settings`, clicked Andreas's own role switch (Owner → Limited). Observed: `PATCH /api/v1/household/members/<andreas-membership-id>` returned **`409 Conflict`**, body exactly `{"error":{"code":"LAST_OWNER","message":"A household must keep at least one owner."}}`. The UI rendered "A household must keep at least one owner." directly under his row, and his role switch stayed `checked`/"Owner" — nothing changed. This is the cleanest possible confirmation: no workaround, no restructuring, the criterion exactly as originally worded, against a household in exactly the state it assumed.

**PASS**, verified by substance, both here (literally, against a genuine sole owner) and previously (Step B, after deliberately restoring that precondition). The "remove" half of "remove or demote" still has no UI path (unchanged, and out of scope per the coordinator's ruling: its absence matches the design, not a defect).

---

## The full gate (after the fixes)

### `make lint`

```
$ make lint
./scripts/arch-lint.sh
architecture lint passed
cd web && npx tsc --noEmit
cd web && npm run lint

> web@0.0.0 lint
> eslint .

cd api && go vet ./...
```

Clean, no findings. **PASS.**

### `make test`

```
$ make test
cd api && go test ./... -count=1 -timeout=5m
ok  	github.com/andreasoentoro/hearth/api/cmd/adminctl	0.443s
ok  	github.com/andreasoentoro/hearth/api/cmd/api	1.935s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/clock	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/crypto	1.322s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/fx	1.371s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/http	18.503s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/mail	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/postgres	26.360s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/config	2.194s
ok  	github.com/andreasoentoro/hearth/api/internal/domain	3.055s
?   	github.com/andreasoentoro/hearth/api/internal/testsupport	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/usecase	3.410s
cd web && npx vitest run

 RUN  v4.1.10 /Volumes/Oink_Machine/Intelij/HouseholdDashboard/web

Not implemented: Window's scrollTo() method
[... jsdom warnings, benign, x6 ...]

 Test Files  13 passed (13)
      Tests  82 passed (82)
   Start at  03:44:46
   Duration  1.69s (transform 616ms, setup 1.35s, import 1.48s, tests 2.50s, environment 5.64s)
```

82 tests (up from 76): +2 `mutationObserverCallbacks.test.ts`, +1 `MagicLinkConsumeScreen.test.tsx`, +3 `CurrencyPanel.test.tsx` net (4 added, 1 renamed/adjusted, none removed). All green. **PASS.**

Environment reset used for this gate and the re-walk above: `make down`, `docker volume rm hearth_hearth-pgdata`, `make up`, `make seed` — a genuinely independent clean start from the original walkthrough's, not a continuation of its mutated state. Torn down again afterward with `make down`.

---

## The original gate (pre-fix, for the historical record)

### `make lint`

```
$ make lint
./scripts/arch-lint.sh
architecture lint passed
cd web && npx tsc --noEmit
cd web && npm run lint

> web@0.0.0 lint
> eslint .

cd api && go vet ./...
```

All four steps (`lint-arch`, `typecheck`, `lint-web`, `go vet`) completed with no findings and exit 0. **PASS.**

### `make test`

```
$ make test
cd api && go test ./... -count=1 -timeout=5m
ok  	github.com/andreasoentoro/hearth/api/cmd/adminctl	0.466s
ok  	github.com/andreasoentoro/hearth/api/cmd/api	1.920s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/clock	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/crypto	1.898s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/fx	2.707s
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/http	17.672s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/mail	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/adapter/postgres	25.524s
?   	github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/config	2.334s
ok  	github.com/andreasoentoro/hearth/api/internal/domain	3.074s
?   	github.com/andreasoentoro/hearth/api/internal/testsupport	[no test files]
ok  	github.com/andreasoentoro/hearth/api/internal/usecase	3.457s
cd web && npx vitest run

 RUN  v4.1.10 /Volumes/Oink_Machine/Intelij/HouseholdDashboard/web

Not implemented: Window's scrollTo() method
[... jsdom warnings, benign, x6 ...]

 Test Files  12 passed (12)
      Tests  76 passed (76)
   Start at  03:12:34
   Duration  1.80s (transform 783ms, setup 1.43s, import 1.40s, tests 2.44s, environment 6.51s)
```

Every Go package (using real Postgres via testcontainers, run with `DOCKER_HOST` and `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE` pointed at colima's socket) passed. All 12 frontend test files / 76 tests passed. **PASS**, exit 0 for both suites.

**This green result coexists with the criterion-4 defect above, and that is not a contradiction.** The unit/component suite is real and passing; it simply doesn't (and structurally can't, per `MagicLinkConsumeScreen.test.tsx`'s harness) exercise a live multi-route browser navigation. `make test` measures what it measures; this walkthrough is what caught the rest.

---

## Summary of the original walkthrough (pre-fix)

**Tally: 7 PASS, 3 FAIL** (criteria 1, 2, 3, 5, 7, 8, 10 pass; criteria 4, 6, 9 fail), plus `make lint` and `make test` both green. Superseded by the final summary below, which reflects the fixes and re-walk — kept here as the historical record of what was actually found before anything was changed.

**Environment:** reset from nothing (`make down`, volume removed, `make up`, `make seed`) and torn down again at the end (`make down`) exactly as instructed. Colima/Docker, Mailpit, and Postgres all behaved correctly throughout.

**What works, end to end, in a real browser:**
- Accepting an invite and setting a password (criterion 1).
- Signing out and back in with a password (criterion 2).
- The lockout mechanism itself — three wrong attempts really do lock password sign-in for 15 minutes, with the exact designed copy at each step, and a locked household really does keep rejecting password sign-in on a direct 4th attempt (criterion 3, modulo an off-by-one in the brief's own attempt-count prose, not in the system).
- Magic-link sign-in working while the household is locked — the backend recovery path is real and untouched by the lock (criterion 4's mechanism, though not its UI redirect — see below).
- The sidebar rendering exactly the spaces the server sends, correctly filtered by capability, verified for both an owner and a genuinely capability-restricted account (criterion 5).
- Toggling and persisting every notification preference (part of criterion 6).
- Inviting a new member and having the email actually arrive in Mailpit, for both a parent and a child invite (criterion 7).
- Removing a capability from a limited member and having it persist (criterion 8).
- The `LAST_OWNER` rule itself, proven with an exact 409/`LAST_OWNER` response against a genuine sole owner (part of criterion 9).
- Sign-out correctly gating `/` behind `/sign-in` (criterion 10).
- The full `make lint` and `make test` gate, both green.

**What does not work, or cannot be exercised as specified:**
1. **Magic-link sign-in never redirects itself in the browser (criterion 4).** The backend authenticates correctly and the session is valid, but `MagicLinkConsumeScreen` gets stuck on "Signing you in…" forever with no error and no navigation — reproduced twice, cleanly. A real user has no way to know they're already signed in. This is the single most user-facing defect found.
2. **There is no UI control to change the primary currency, at all (criterion 6).** `CurrencyPanel.tsx` renders it as static text by design, mirroring the design mockup's own non-interactive treatment. The backend supports the change fully (verified directly); only Settings has no path to it.
3. **There is no "remove a member" control anywhere in the UI (criterion 9).** Only role demotion is possible. `DELETE /api/v1/household/members/:id` exists server-side but nothing in Settings can reach it.
4. **Criterion 9's stated precondition ("while he is the only owner") is false as soon as criterion 1 completes**, because Christine's seeded invite is also `role: owner`, and there is no "remove member" control in the UI regardless. Both make the criterion, as written, impossible to execute against Andreas specifically. This is graded FAIL for that reason alone — separately, and unambiguously, the underlying `LAST_OWNER` rule was verified directly against a genuine sole owner (Christine, after Andreas was demoted for the test) and works correctly: an exact `409`/`LAST_OWNER` response, nothing changed in the UI. See criterion 9 for the full account of both.

Note that "criterion 3's brief text doesn't match the shipped attempt-count" and "criterion 9's precondition doesn't hold" are the same category of problem — the brief's description of the system diverges from the system's actual, correct behavior — but they're graded differently on purpose: criterion 3's substance (three specific messages, in order, ending locked) all happened, just one attempt earlier than described, so it's PASS with the discrepancy noted; criterion 9's substance (a blocked action against the stated target under the stated precondition) could not happen at all, so it's FAIL even though the rule it's testing works.
5. **The brief's attempt-count prose for criterion 3 is off by one** from the shipped, unit-tested lockout policy: the household locks on the 3rd wrong attempt, not needing a 4th, and "Two tries left" appears after the 1st wrong attempt, not the 2nd. (Graded PASS — see the note above.)

**Nothing was left unverified for lack of trying.** Every criterion was either directly confirmed, or — where the literal instruction couldn't be carried out (no currency control, no remove control, false precondition) — supplemented with the most direct alternative verification available (a raw authenticated API call, or a restructured sequence that restores the intended precondition) so the underlying rule's correctness or incorrectness is still known, not just assumed.

**Database end state, left honest rather than cleaned up — but note this describes a *different* run than the one that produced it.** This paragraph, at the time it was first written, described the original walkthrough's volume: Tessa a real member, Priya's invite pending, all four notification preferences and `showSecondaryCurrency` off. That volume no longer exists — it was destroyed by `docker volume rm hearth_hearth-pgdata` before the fixes' re-walk (a deliberate second clean start, not an accident), so none of it persists.

What actually persists now is the re-walk's own volume, last touched during Fix 2's re-walk and criterion 9's re-walk. Concretely: Christine's invite is still pending and unaccepted (deliberately not accepted in this pass — see criterion 9's re-walk); no Tessa, no Priya (neither of that run's supplementary steps was repeated here); all four notification preferences and `showSecondaryCurrency` remain at their seeded defaults (never touched in this pass); `primaryCurrency` is back to `SGD` (typed `usd` then `zzz` during the re-walk, reverted to `sgd` afterward via the same control — see Fix 2's re-walk); Andreas is still the household's sole owner (the criterion-9 re-walk's demotion attempt was blocked by `LAST_OWNER`, so nothing changed); the household also carries three recorded failed sign-in attempts and two consumed, single-use magic-link tokens from this pass's own re-walk of criteria 3 and 4. Anyone reusing this environment without `docker volume rm hearth_hearth-pgdata` first will see this state, not the one `make seed`'s own printed output describes, and not the original walkthrough's state described above either.

Torn down at the end with `make down`; all five containers (`web`, `api`, `migrate`, `postgres`, `mailpit`) and the `hearth_default` network stopped and removed cleanly. The volume itself was intentionally left alone in this final teardown (the brief's Step 4 only calls for `make down`, not a volume removal), so the state above is what persists until someone runs `docker volume rm hearth_hearth-pgdata` again.

---

## Final summary (after fixes and re-walk)

**Tally: 10 PASS, 0 FAIL.** All ten criteria pass; `make lint` and `make test` both green (82 tests, up from 76).

- Criteria 1, 2, 3, 5, 7, 8, 10: unchanged from the original walkthrough — PASS (see their sections above for the full account).
- **Criterion 3 carries a discrepancy, recorded here explicitly rather than folded silently into its PASS**: the shipped, unit-tested lockout policy locks the household one attempt earlier than the task brief's own prose describes ("Two tries left" appears after the **1st** wrong attempt, not the 2nd; the household locks on the **3rd** wrong attempt, not needing a 4th). The shipped behavior is correct and intentional (pinned by `TestOneFailureLeavesTwoTries` in `api/internal/domain/lockout_test.go`); the brief's wording is what's wrong. This does not change the verdict — the substance of what the criterion checks (three specific messages, in order, ending locked, confirmed locked on a further attempt) all happened — but it is a real mismatch between spec and system and is called out as its own line so it isn't quietly corrected away.
- Criterion 4 (magic-link consume never navigating): **fixed** — see "Fix 1" above. Re-walked twice, independently, in a real browser against a fresh clean start: request while locked → follow from Mailpit → lands signed in with no action beyond following the link. The reliable-vs-unreliable asymmetry is confirmed, not guessed: `MutationObserver`'s per-call `.mutate()` callbacks are gated on `hasListeners()`, while the mutation's own `onSuccess`/`onError` (set at `useMutation({...})` construction) are awaited unconditionally by `Mutation#execute()`. The bug's call site fired from a mount effect, the working comparison case (`AcceptInviteForm`) fires from a form submit — StrictMode's mount-effect teardown is the leading candidate for why the subscription was absent at settle time, but that is not fully nailed down (see "Fix 1" above for the specific gap in that theory). Fixed by moving the side effects (navigate, cache priming, error surfacing) onto the mutation's own `onSuccess`/`onError`, which is reliable regardless of which explanation is right.
- Criterion 6 (no primary-currency control): **fixed** — see "Fix 2" above. Re-walked in a real browser: an owner can now edit the primary currency (validated client-side to match the backend's own three-uppercase-letter rule), save it, and see it persist across a reload; a rejected save surfaces the backend's real message inline. Surfaced, in passing, that the backend's currency validation is format-only (`ZZZ` is accepted) — a pre-existing characteristic of `domain.NewMoney`, not something this fix introduced or was asked to change.
- Criterion 9 (unexecutable precondition): **re-walked, both by substance and, this time, literally.** No code change was needed — the `LAST_OWNER` rule was already correct. The coordinator ruled this a defect in the criterion's wording (Christine's seed also grants her `owner`, so "Andreas is the only owner" stops being true the moment criterion 1 completes) rather than in the product, and amended it to be verified by substance. The second clean start's timing made a fully literal re-check possible too: with Christine's invite not yet accepted, Andreas genuinely was the household's only owner, and attempting to demote him was blocked with an exact `409`/`LAST_OWNER` response and no change to the UI — the criterion exactly as originally worded, satisfied without any restructuring. The "remove" (as opposed to "demote") half of the control still doesn't exist in the UI; per the coordinator's explicit ruling, that absence matches the design and is not a defect to fix.

**Commits:** the two fixes (magic-link navigation, primary-currency control) are in `b71db7c487272c8ffa3eb967e4b5f8594cecb9e7` (`fix: navigate after magic-link consume; add primary-currency control`); this document's update is in a separate commit (`docs: ...`, see the repository log for its SHA — it necessarily postdates this line, since the SHA cannot be known before that commit exists).
