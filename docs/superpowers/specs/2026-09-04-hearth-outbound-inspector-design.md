# Hearth — the outbound message inspector

**Written 2026-09-04.** This expands §5 of
`docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` the way
`2026-09-02-hearth-admin-households-design.md` expanded §6. **Where the two
differ, this one wins**, and §13 lists every difference so nobody has to
diff them by hand.

It is the third of the operator surface's four features and the second of
the two that remain. The read-only database browse (§4 of the admin-surface
spec) comes after it, deliberately: this one has no infrastructure
dependency, and it gives the re-authentication grant and the audit log more
real use before the surface that can read every household's finances
arrives.

---

## 1. What this is, and the pain it closes

Under [ADR 3](../../adr/0003-mail-stays-on-the-box.md) no mail leaves the
production box. Mailpit catches every message the product sends and is bound
to loopback. So today, handing a new member their invite means what
`deploy/README.md` describes: open an SSH tunnel from your laptop to
Mailpit's web UI, find the message, copy the link out by hand, and send it
through some other channel.

The inspector is that, in the operator surface, with an audit row. It lists
what Hearth has sent, and — on a deliberate second click — shows one
message's links so the operator can copy one.

**It closes an operational pain. It does not add a product capability.** No
household member can see any of it.

## 2. What it is not

- **Not a mail archive.** It reads Mailpit's store, which is
  ephemeral — see decision 9.
- **Not a template previewer.** It never renders an email as the recipient
  sees it; see decision 1.
- **Not a Telegram inspector.** Sign-in and sign-up links delivered by the
  bot never touch Mailpit, and that is correct: this feature exists for mail
  that *cannot* leave the box, and Telegram delivers. Stated here so nobody
  files its absence as a gap.
- **Not a search.** See decision 8.
- **Not a way to delete, release, or resend anything.** Two reads, no writes.

---

## 3. Decisions

### Decision 1 — Opening a message shows extracted links plus the plain-text body, never rendered HTML

The operator's actual job is "hand this person their link". So the message
view shows every URL found in the body as a list, each with a copy control,
and the plain-text body below it for context.

The rejected alternative is rendering the email as the recipient sees it.
It buys fidelity for debugging templates, and it costs a sanitisation
problem inside the one surface that can read every household — for a
convenience the link list already delivers in one click. The operator who
genuinely needs to see an email's rendering still has the SSH tunnel to
Mailpit's own UI; that path is not removed, only made unnecessary for the
common case.

**Product owner's decision, 2026-09-04.**

### Decision 2 — `link-check` must never be called

Mailpit exposes `GET /api/v1/message/{id}/link-check`, which looks like
exactly what decision 1 needs. **It is a trap.** It issues a real HTTP
request to every URL it finds (`internal/linkcheck/status.go`, `doHead` per
link, at v1.30.5) to report each one's status code.

Every URL in a Hearth email is a live single-use credential on a public
host: a magic-link token, an invite token, a sign-up token. Asking Mailpit
to check them means asking it to *visit* them. Whether a `HEAD` against
those routes consumes a token today is beside the point — the feature would
be one route change away from silently burning the very links it exists to
hand out.

Hearth extracts links itself, from a body it has already fetched, touching
nothing.

### Decision 3 — Extraction lives in `domain`, not in the adapter

`domain.ExtractLinks` is a pure function over strings: standard library
only, no HTTP, no Mailpit. The Mailpit adapter's whole job is to speak HTTP
and JSON and hand back what it was given.

The alternative — the adapter returning links it extracted itself — puts a
policy decision ("what counts as a link") inside an HTTP client, the layer
with the least testable seams in this codebase. Extraction is the
interesting logic here, so it belongs in the layer with no dependencies and
the cheapest tests.

### Decision 4 — Text first, HTML only as a fallback

`SMTPMailer.send` builds every message with
`msg.SetBodyString(gomail.TypeTextPlain, body)`
(`api/internal/adapter/mail/smtp.go`). **Hearth sends plain text and
nothing else**, so Mailpit's `HTML` field is empty for every message this
product sends today.

`ExtractLinks(text, html string) []string` therefore reads `text` when it is
non-empty and falls back to `html` only when it is not. The fallback is not
speculative generality: Mailpit's store holds whatever spoke SMTP to it, and
a message showing an empty link list would read as a broken feature rather
than as a message with no links. The fallback path unescapes HTML entities
(`html.UnescapeString`) before matching, because an `href` writes `&amp;`
where the URL has `&`.

If Hearth's templates ever gain an HTML part, this function already handles
it and its tests already say what it does.

### Decision 5 — A separate service, following `AdminDirectoryService`

`AdminOutboxService` is its own service rather than three more methods on
`AdminService`. The precedent is explicit in `usecase/admin_directory.go`:
`AdminService` is "who is a platform admin, feature flags, the audit log",
and a service that reads across every household is a different job.

It is small, and it earns its existence: it clamps the limit, composes the
port's raw message with `domain.ExtractLinks`, and is the seam where "HTML
never leaves the usecase layer" is enforced.

### Decision 6 — The port returns what Mailpit gave; the service returns the view

Two types, and the split is the whole point of decision 3.

```go
// usecase/ports.go

// MailOutbox reads messages the product has sent. It exists so the operator
// can hand someone a link that mail cannot deliver (see ADR 3). The only
// implementation today reads Mailpit; when mail leaves the box this port
// gets a second one instead of a rewrite.
//
// Message returns domain.ErrNotFound for a message the outbox does not
// hold. Both methods return usecase.ErrOutboxUnavailable -- declared beside this port,
// because it is part of the contract rather than of any implementation --
// when the outbox itself cannot be reached --
// the caller must be able to tell "no such message" from "Mailpit is down",
// because the operator needs different advice in each case.
type MailOutbox interface {
    // Recent returns up to limit messages, newest first, with bodies left
    // empty. A list never carries a body: see decision 7. It returns a page
    // rather than a slice because the screen says "showing 50 of 128", and
    // the total is Mailpit's to report -- a caller cannot infer it from a
    // slice that is exactly as long as the limit it asked for.
    Recent(ctx context.Context, limit int) (OutboxPage, error)
    // Message returns one message including both body parts, exactly as the
    // outbox holds them. Extraction is the caller's job.
    Message(ctx context.Context, id string) (OutboxMessage, error)
}

// OutboxPage is one screenful of the outbox. Total is how many messages the
// outbox holds altogether, not how many were returned; the service turns the
// difference into Truncated.
type OutboxPage struct {
    Messages []OutboxMessage
    Total    int
}

// OutboxMessage is a message as the outbox holds it. Text and HTML are
// populated by Message and empty in Recent.
type OutboxMessage struct {
    ID      string
    To      string
    Subject string
    SentAt  time.Time
    Text    string
    HTML    string
}
```

The service returns a different shape, and **`HTML` is not on it**:

```go
// usecase/admin_outbox.go

// OutboxMessageView is one message as the operator sees it: the links pulled
// out of whichever body part had them, and the plain text for context. The
// HTML part never reaches the HTTP layer -- nothing above this service has a
// use for it, and the surface that renders it is the one this design
// rejected (decision 1).
type OutboxMessageView struct {
    ID      string
    To      string
    Subject string
    SentAt  time.Time
    Text    string
    Links   []string
}
```

### Decision 7 — The list carries no body, not even a snippet

Mailpit's list response includes `Snippet`, up to 250 characters of body
text. Hearth's list ignores it.

A sign-up email is short enough that 250 characters can contain the whole
link. Putting it in the list would make seeing a live credential the default
state of the screen rather than a deliberate act with its own audit row,
which is the property §5.3 of the parent spec exists to protect. The list
shows recipient, subject and time. Nothing else.

### Decision 8 — No search in v1

The list is newest-first, `limit` clamped exactly as the directory clamps
its own (default 50, maximum 200 — `usecase/admin_outbox.go` declares its
own constants rather than importing the directory's, because the two limits
answer different questions and coupling them would make one change move the
other).

The real install has two people. Mailpit's store is ephemeral and small. A
search adds a query parameter to an audited route, a second Mailpit endpoint
(`GET /api/v1/search`), and its own escaping rules, to solve a problem the
first screenful already solves. If the install grows to where the newest 50
are not enough, the endpoint is there and this decision gets revisited with
evidence.

### Decision 9 — "Recent" means whatever Mailpit still holds, and that is not durable

The production Mailpit service has **no volume**
(`deploy/docker-compose.prod.yml`, the `mailpit` block). A container restart
— a deploy, a reboot, an OOM — loses every message it held.

This is not a defect to fix here. It is the honest boundary of the feature,
and it strengthens §5.1's argument: nothing durable is being built, no new
store of live credentials exists, and a link that has aged out of Mailpit is
recovered the way it always was — by sending a new one.

**The screen says so**, in one line under the list, so an operator who finds
an expected message missing does not go looking for a bug.

### Decision 10 — Unconfigured and unreachable are two different answers

`MAILPIT_API_URL` unset means the panel is unavailable. That must never
degrade to an empty list, which would read as "Hearth has sent no mail".

| State | Answer | Code | What the screen says |
|---|---|---|---|
| `MAILPIT_API_URL` unset | `503` | `MAIL_INSPECTOR_NOT_CONFIGURED` | The inspector is not configured on this install, and names the variable |
| Mailpit unreachable, timing out, answering non-2xx **other than 404**, or answering a body the adapter cannot map (a message with no recipient, say — see §6) | `502` | `MAIL_UPSTREAM_UNAVAILABLE` | Mailpit is not answering usefully; the messages are not lost, the reader is |
| Message id not in Mailpit's store | `404` | `NOT_FOUND` | This message is no longer in Mailpit — see decision 9 |
| Message id not 22 characters of `[0-9A-Za-z]` | `400` | `INVALID_ID` | Refused before any upstream request is made — decision 12 |

Neither unavailability answers `404`. `requireFeature`'s `404` and
`requirePlatformAdmin`'s `404` both exist to hide a route's *existence* from
someone who should not know it is there. Everyone who can reach this handler
has already proved they are a platform admin with a live grant; hiding the
route from them would only cost them the one piece of information that tells
them what to fix.

**This defines the fail-closed shape §5.2 says the database browse shares.**
The browse copies this; this does not wait for it.

### Decision 11 — `Deps.AdminOutbox` is nil when unconfigured, and the route is registered anyway

The same shape `Telegram` already has (`router.go`'s `Deps` comment): the
route tree does not change with configuration, so every test builds the same
tree and the handler's nil check is the one place the decision lives.

### Decision 12 — The message id is validated before it reaches the upstream URL

Mailpit ids are exactly 22 characters of `[0-9A-Za-z]`
(`internal/shortuuid/shortuuid.go` at v1.30.5: a 22-character alphanumeric
alphabet). The handler refuses anything else with `400 INVALID_ID` before
any request is made.

This is `CLAUDE.md`'s "fail closed on values you did not construct", and it
has two specific teeth here: Mailpit treats the literal id `latest` as "the
most recent message", which would make a typo return someone else's mail;
and an id containing `/` or `..` would let a request aimed at one message
reach a different Mailpit endpoint entirely. The adapter additionally
path-escapes the segment — belt and braces, because the validation and the
URL construction are in different files and only one of them can be looked
at during a change.

### Decision 13 — The audit row names the message, not the recipient

§5.3 of the parent spec says opening a message writes "its own audit row
naming the recipient and message ID". **The recipient half is not
buildable as described, and this design drops it rather than bending the
log's shape.**

`auditAdmin` writes its row *before* chi has matched the route
(`middleware_admin.go`), which is deliberate and load-bearing: a handler
that panics still leaves a trace. The consequence is that route parameters
do not exist yet, so `Detail` carries the raw query string and nothing else.
The recipient is not knowable at that point.

What survives, and it is the part that matters:

- **The message id is in the row**, because `Target` is the full request
  path and the id is in the path.
- **Opening a message is its own row**, distinct from listing.
- **The row never contains a body, a link, or a token** — the path has no
  room for one and `Detail` is `{}` on a URL with no query.

The recipient is recoverable by opening the same message again, for as long
as Mailpit holds it. Buying it at audit-write time would mean either a
second row per view (double-counting the one action the log exists to make
countable) or a handler-to-middleware enrichment channel, which is a new
mechanism in the log's hot path to record a fact that is one click away.

### Decision 14 — No feature flag

Configuration presence is the gate, as it is for Telegram. A flag would add
a second switch that can disagree with the first, and pattern 8 of
`docs/LEARNING.md` is a list of what happens when two things have to agree
and nothing makes them.

### Decision 15 — Reading a message marks it read in Mailpit

`GET /api/v1/message/{id}` marks the message read in Mailpit's own store
("Returns the summary of a message, marking the message as read", v1.30.5).

So a panel described as read-only does cause a write — in Mailpit, to a flag
that is not product state and that nothing in Hearth reads. That is
acceptable, and it is written down here rather than discovered by someone
comparing Mailpit's unread count before and after. Avoiding it would mean
fetching the raw source and parsing MIME ourselves, which is a large amount
of code to protect a flag nobody uses.

---

## 4. The layers

```
domain/outbox_links.go                    ExtractLinks — stdlib only, pure
usecase/ports.go                          MailOutbox, OutboxMessage
usecase/admin_outbox.go                   AdminOutboxService, OutboxMessageView
adapter/mail/mailpit_outbox.go            the one implementation
adapter/http/admin_outbox_handlers.go     two handlers, granted group
web/src/features/admin/AdminMailPage.tsx  list and message view
web/src/features/admin/useAdminOutbox.ts  two queries
web/src/features/admin/adminOutboxSchemas.ts
```

Dependencies point inward, as everywhere: `ExtractLinks` knows nothing about
Mailpit, `AdminOutboxService` knows nothing about HTTP, and no Mailpit type
crosses out of `adapter/mail`.

## 5. `domain.ExtractLinks`

```go
// ExtractLinks returns every http and https URL in a message body, in the
// order they appear, with duplicates removed. It reads text when text has
// content and falls back to html otherwise -- Hearth sends text/plain only
// (adapter/mail/smtp.go), so the html path is for messages this product did
// not send.
func ExtractLinks(text, html string) []string
```

Rules, each of which is a test:

- **`http` and `https` only.** A `mailto:` is not a link the operator can
  hand someone.
- **Source order preserved, duplicates removed.** A magic-link email that
  names its URL twice shows one entry.
- **No host filter.** Hearth's own templates contain only Hearth URLs, and
  filtering on a configured base URL would make this function depend on
  configuration to do something it does not need configuration for.
- **Trailing punctuation is not part of the URL, and the strip set is
  written out rather than described.** `…visit https://x/y.` ends the URL at
  `y`. Strip only `. , ; : ! ?` and the closing brackets `) ] > "` — and
  **never `-` or `_`**.

  This is the rule most likely to be "tidied" into a general
  non-alphanumeric strip, and doing so would silently break links. Tokens
  are `base64.RawURLEncoding` (`adapter/crypto/tokens.go`), whose alphabet
  is `A-Za-z0-9-_`, and both link shapes put the token last:
  `<base>/invite/<token>` and `<base>/sign-in/magic?token=<token>`. A strip
  set containing `-` or `_` therefore eats the final character of roughly
  one token in thirty-two, producing a link that looks right, copies right,
  and fails on use with nothing red anywhere.

  So the table test carries **two deliberate fixtures: a token ending in `-`
  and a token ending in `_`**, alongside Hearth's four real templates. A
  captured real token would hit those endings only by luck, which is exactly
  why they are constructed rather than captured.
- **HTML entities unescaped on the fallback path only.**

## 6. HTTP surface

Both routes join the `granted` group in `router.go`, so
`requirePlatformAdmin`, `auditAdmin`, `requireCSRF` and `requireAdminGrant`
all apply by construction — nothing in either handler checks who is asking.

```
GET /api/v1/admin/mail?limit=N     list, newest first
GET /api/v1/admin/mail/{id}        one message
```

```jsonc
// GET /admin/mail
{
  "messages": [
    { "id": "0OQ1sV2mB7hN4kR8xT3wZq", "to": "chris@example.com",
      "subject": "Your Hearth sign-in link", "sentAt": "2026-09-04T09:12:33Z" }
  ],
  "total": 12,        // what Mailpit holds, so the screen can say "12 of 12"
  "truncated": false  // Mailpit holds more than limit returned
}

// GET /admin/mail/{id}
{
  "id": "0OQ1sV2mB7hN4kR8xT3wZq",
  "to": "chris@example.com",
  "subject": "Your Hearth sign-in link",
  "sentAt": "2026-09-04T09:12:33Z",
  "links": ["https://oink.mywire.org/sign-in/magic?token=…"],
  "text": "Hi Chris,\n\nHere is your sign-in link…"
}
```

Every 2xx carries a JSON body, per `CLAUDE.md`. Timestamps are RFC 3339 in
UTC, as the directory's are.

**`sentAt` comes from a different Mailpit field on each route, and it has
to.** `MessageSummary` (the list) carries `Created`, when Mailpit received
the message, and no `Date`. `Message` (the detail) carries `Date`, the
message's own header falling back to received time, and no `Created`. Neither
field is on both responses, so there is no single source to pick.

The two can therefore differ in principle. In practice they do not: the SMTP
hop is to a container on the same host, and Hearth's own mail client stamps
`Date` at send. The adapter comments both mappings and names this as the
reason the values are treated as interchangeable, so the next person to see
a one-second difference between two screens knows it is the hop and not a
bug.

**`to` is the first recipient.** Mailpit models `To` as a list; Hearth sends
to exactly one address in all four templates. An empty list is an adapter
error, not a blank cell — a message with no recipient means the assumption
this line rests on has changed.

## 7. Configuration

`MAILPIT_API_URL`, read in `internal/config/config.go` alongside the
Telegram pair. `http://mailpit:8025` in production — `api` and `mailpit`
share the `hearth` Compose network and `api` already declares
`env_file: [.env]` and `depends_on: mailpit`, so **no compose change is
needed**, only the variable.

Empty means the feature is off and `Deps.AdminOutbox` is nil (decision 11).

**A value that is set but unparseable refuses the boot**, the way
`config.go` already refuses a half-set Telegram pair. The alternative is a
typo in `.env` presenting on the box as a `502` with nothing pointing at the
`.env` line that caused it — pattern 8's exact shape.

The upstream client uses a **5-second timeout** and no retries: it is a
request to a container on the same host, so a slow answer means something is
wrong rather than something is far away, and the operator is better served
by a `502` that says so than by a page that hangs.

`docs/INFRASTRUCTURE.md` already names `MAILPIT_API_URL` as the variable
this unbuilt feature will need. This work makes that sentence true — until
it lands, that line is exactly the kind of configuration claim pattern 8
warns about.

## 8. Frontend

`/admin/mail`, a third link in `AdminShell`'s `OperatorNav`, between Flags
and Households, computing its own active state with `useMatchRoute` — never
`activeProps`, for the reason `docs/LEARNING.md`'s Frontend section records.

The page follows `AdminHouseholdsPage` exactly, because that shape is
already walked and already correct:

- `useCloseSurfaceOnReauth(query.error)` so a grant that lapses mid-read
  reaches the one `AdminGate` the shell owns.
- `isAdminLayerFailure(error)` filters the gate's own codes out of the
  page's inline error, so a `502` or `503` renders as page copy rather than
  painting the password prompt.
- **`refetchOnWindowFocus: false` on both queries.** Every request under
  `/admin` writes an audit row before its handler runs, so the default —
  refetch on tab focus — turns an alt-tab into a logged read and buries the
  rows the log exists to surface. This is not a new insight: it is the one
  thing that survived the descoped audit screen, recorded on `useAdminFlags`,
  and applying it before the defect exists is the sibling-defect rule doing
  its job.
- **Retries stay off, and are already off globally** — `main.tsx` sets
  `retry: false` on every query. Checked rather than assumed, because it is
  the same class of problem: three retries on a `503` would be four audit
  rows per failed load and several seconds of spinner before the
  unavailability copy this design wrote ever appears. Neither query sets its
  own `retry`, and neither should.
- The new page must land in the admin bundle chunk —
  `adminBundleSplit.test.ts` is the test that says so.

The message view is a route, not a modal: it is a distinct audited request,
and a URL an operator can return to is worth more here than an overlay.

Each link renders with a copy control. **Nothing on either screen renders
HTML from a message body.**

## 9. Testing

| Level | What it covers |
|---|---|
| `domain/outbox_links_test.go` | Table test over the four real Hearth templates plus the punctuation, duplicate, ordering, `mailto:`, entity-unescape and empty-body cases |
| `usecase/admin_outbox_test.go` | Service against an in-memory `MailOutbox` double: limit clamped at both ends, the not-configured and unavailable paths, and **`OutboxMessageView` never carrying HTML** |
| `adapter/mail/mailpit_outbox_test.go` | Against an `httptest.Server` serving captured real Mailpit v1.30.5 JSON: field mapping, the `sentAt` source choice, first-recipient rule, non-2xx becoming `ErrOutboxUnavailable`, 404 becoming `domain.ErrNotFound`, and the id being path-escaped |
| `adapter/http/admin_outbox_api_test.go` | All four error codes of decision 10's table; the list response's **exact key set**, asserted the way the directory tests assert no money leaks, so a body or snippet cannot be added without a red test; `INVALID_ID` for a 21-character id, a 23-character id and `latest` |
| `web/.../AdminMailPage.test.tsx` | The two unavailability copies, the link list, the copy control, and that no `dangerouslySetInnerHTML` appears |

At least one test per task is mutation-checked, per `CLAUDE.md`.

**The test that must exist and would be easy to omit**: nothing calls
`link-check`. A grep-style assertion in the adapter test that the only
upstream paths requested are `/api/v1/messages` and `/api/v1/message/{id}`
— the `httptest.Server` already sees every request, so it can fail on any
third path. Decision 2 is the kind of rule that survives review and dies in
a later refactor unless a test holds it.

## 10. Documentation this work updates

Part of the work, not a tidy-up after it:

- `docs/SYSTEM_DESIGN.md` — a new port, a new adapter, two new routes and a
  new screen. Use the `maintaining-system-design` skill.
- `docs/FEATURE_TRACKER.md` — section 9's "Outbound message inspector" row
  ⬜ → ✅ (or 🟡 with the gap named), and the summary table recounted from
  the symbols rather than adjusted.
- `docs/LEARNING.md` — the `link-check` trap belongs in the log whether or
  not anyone trips it, because the next person reading Mailpit's API will
  find that endpoint and think it is a gift.
- `docs/INFRASTRUCTURE.md` — `MAILPIT_API_URL` becomes real.
- `deploy/.env.example` — the variable, with a comment saying what is
  unavailable without it.
- `deploy/README.md` — the tunnel section stops being the only way to hand
  out a link.
- `docs/ADMIN_SURFACE_HANDOVER.md` — §2 gains the feature, §3 loses it.

## 11. Rollout

One branch, `admin-outbox`, in dependency order: domain, port, service,
adapter, config, routes, frontend, docs, then the browser walk. The plan
turns this into tasks; it is not written here.

**Definition of done is `CLAUDE.md`'s**: `make lint && make test` green, at
least one mutation-checked test per task, tracker and learning log updated,
and — the bar the product owner set explicitly — **a fifteen-criterion
browser walk against the running stack**, recorded criterion by criterion,
before anyone calls it done.

Two criteria that walk badly from a script and must be written carefully:
the not-configured state needs the API restarted with the variable unset,
and the unreachable state needs Mailpit stopped while the API stays up.
Both are the states that will actually be met on the box, and both are
invisible to a green test suite.

## 12. Rejected options

| Option | Why not |
|---|---|
| Mailpit's `link-check` endpoint | Visits every URL in the message. Decision 2. |
| Storing sent links in Postgres | Creates a store of live credentials to solve a convenience problem. §5.1 of the parent spec. |
| Rendering the HTML body | Sanitisation surface inside the surface that reads every household, for fidelity the tunnel still provides. Decision 1. |
| Extracting links in the browser | Ships the body over the wire and puts security-shaped parsing in TypeScript. Decision 3. |
| A search box | Two-person install, ephemeral store, audited route. Decision 8. |
| A link from the household drill-in | Mailpit knows addresses; the drill-in knows household ids. The join does not exist and is not needed. |
| A feature flag | Two switches that can disagree. Decision 14. |

## 13. Differences from admin-surface spec §5

| §5 says | This says | Why |
|---|---|---|
| `OutboxMessage.Body string`, populated by `Message` | `Text` and `HTML` on the port; `Text` and `Links` on the service's view type | Decision 1 changed what the screen shows; decision 3 moved extraction to the domain, so the port must hand back both parts unprocessed |
| The audit row names "the recipient and message ID" | The row names the message id, via `Target`; the recipient is not recorded | `auditAdmin` writes before chi routes, so route parameters do not exist at that point. Decision 13 |
| "the panel is unavailable and says so, the same fail-closed shape as the DB browse" | Two distinct answers, `503` unconfigured and `502` unreachable, and this spec defines the shape the browse will copy | The browse is unbuilt; a spec cannot inherit a shape from something that does not exist. Decision 10 |
| Silent on the list's contents beyond "recipient, subject and time" | Explicitly no `Snippet` | Mailpit's snippet can contain a whole link. Decision 7 |
| Silent on durability, search, flags, read-marking | Decisions 9, 8, 14, 15 | Each is a question the implementer would otherwise answer alone |
| `Recent` returns `([]OutboxMessage, error)` | `Recent` returns `(OutboxPage, error)` | Found while writing the plan: §6's list response promises `total` and `truncated`, and a bare slice cannot carry either. Mailpit's list response reports the total, so the port passes it through |
