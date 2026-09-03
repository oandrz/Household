# Outbound message inspector — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the platform operator a screen that lists the mail Hearth has
sent and, on a deliberate second click, shows one message's links so they can
hand someone an invite without opening an SSH tunnel to Mailpit.

**Architecture:** A new `MailOutbox` port with one adapter that reads
Mailpit's HTTP API; link extraction as a pure `domain` function so the
adapter stays a JSON client; a small `AdminOutboxService` that composes the
two and is the seam where the HTML body stops; two read routes inside the
existing `/admin` granted group, so all four admin guards apply by
construction; one lazily-loaded React page following `AdminHouseholdsPage`.

**Tech Stack:** Go 1.24, chi, `net/http` + `encoding/json` (no new Go
dependency), React 19, TanStack Query and Router, Zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-04-hearth-outbound-inspector-design.md`
— read it before Task 1. This plan argues from it and does not repeat its
reasoning.

**Branch:** `admin-outbox`, already created, already carrying the spec.

## Global Constraints

Copied from `CLAUDE.md` and the spec. Every task's requirements implicitly
include this section.

- **Clean architecture, enforced by `make lint-arch` including test files.**
  `internal/domain` imports the standard library only. `internal/usecase` may
  add `internal/domain`. Everything else lives under `internal/adapter/**` or
  `cmd/**`. Adapters *may* import `usecase` — that is how they implement its
  ports.
- **No database, HTTP or third-party type crosses out of the adapter layer.**
  No Mailpit type appears outside `internal/adapter/mail`.
- **Authorisation exists only in the HTTP layer.** No service takes an actor
  parameter. Both new routes go inside the existing `granted` group; neither
  handler checks who is asking.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.
- **Fail closed on values you did not construct.** The message id is
  validated before any upstream request is made.
- **Comments say *why*.** Names say what. Exported things carry their
  contract in a doc comment; `usecase/ports.go` is the model.
- **Money is `int64` minor units plus an ISO 4217 code** — no monetary value
  appears anywhere in this feature, and the exact-key-set tests are what keep
  it that way.
- **Definition of done:** `make lint && make test` green, at least one
  mutation-checked test per task, `docs/FEATURE_TRACKER.md` and
  `docs/LEARNING.md` updated, and the browser walk in Task 9 run and recorded.

**Running the suites** (`go` is not on `PATH` in a bare shell on this
machine, and the Go suite needs a Docker socket):

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock

cd api && go test ./... -count=1 -timeout=5m     # or: make test-api
cd web && npx vitest run                          # or: make test-web
make lint                                         # arch lint, tsc, eslint, go vet
```

**A colima or Docker Desktop engine must be running** before any Go test in
this plan. `docker context ls` shows which sockets exist; if neither answers,
start one before Task 1 — every Go package test here uses testcontainers
through `testsupport.StartPostgres`.

---

## File structure

**Created**

| File | Responsibility |
|---|---|
| `api/internal/domain/outbox_links.go` | `ExtractLinks` — pull URLs out of a message body. Pure, stdlib only |
| `api/internal/domain/outbox_links_test.go` | Its table test, including the two constructed token fixtures |
| `api/internal/usecase/admin_outbox.go` | `AdminOutboxService`, `OutboxMessageView`, the limit constants |
| `api/internal/usecase/admin_outbox_test.go` | Service against an in-memory double |
| `api/internal/adapter/mail/mailpit_outbox.go` | The one `MailOutbox` implementation |
| `api/internal/adapter/mail/mailpit_outbox_test.go` | Against an `httptest.Server` serving real Mailpit JSON |
| `api/internal/adapter/http/admin_outbox_handlers.go` | Two handlers and their DTOs |
| `api/internal/adapter/http/admin_outbox_api_test.go` | Route-level tests, exact key sets, the four error codes |
| `web/src/features/admin/adminOutboxSchemas.ts` | Zod mirrors of those DTOs |
| `web/src/features/admin/useAdminOutbox.ts` | The two queries |
| `web/src/features/admin/AdminMailPage.tsx` | List and message view |
| `web/src/features/admin/AdminMailPage.test.tsx` | Its tests |

**Modified**

| File | Change |
|---|---|
| `api/internal/usecase/ports.go` | `MailOutbox`, `OutboxPage`, `OutboxMessage`, `ErrOutboxUnavailable` |
| `api/internal/adapter/http/errors.go` | One `MapDomainError` case: `ErrOutboxUnavailable` → 502 |
| `api/internal/adapter/http/router.go` | `Deps.AdminOutbox`; two routes in the granted group |
| `api/internal/adapter/http/api_test.go` | `newTestEnvWithOutbox`, so a test can supply a double |
| `api/internal/config/config.go` | `MailpitAPIURL` + its validation |
| `api/internal/config/config_test.go` | The unparseable-value refusal |
| `api/cmd/api/main.go` | Build the service when configured, leave `Deps.AdminOutbox` nil when not |
| `web/src/features/admin/AdminShell.tsx` | A third nav link |
| `web/src/routes/router.tsx` | Two lazy routes |
| `web/src/features/admin/adminBundleSplit.test.ts` | Two more absence assertions |
| `docs/*`, `deploy/*` | Task 8 |

---

### Task 1: `domain.ExtractLinks`

The one piece of real logic in this feature, in the layer with no
dependencies. Everything else is plumbing around it.

**Files:**
- Create: `api/internal/domain/outbox_links.go`
- Test: `api/internal/domain/outbox_links_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func ExtractLinks(text, htmlBody string) []string` in package
  `domain`. Task 2's service is its only caller.

- [ ] **Step 1: Write the failing test**

Create `api/internal/domain/outbox_links_test.go`:

```go
package domain_test

import (
	"reflect"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The fixtures are Hearth's four real message bodies (adapter/mail/smtp.go)
// plus the cases that body text alone would never produce. Two of them are
// constructed rather than captured and the reason is in their names: a real
// token ends in "-" or "_" only about one time in thirty-two, so a captured
// sample would pass a broken strip set by luck.
func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name string
		text string
		html string
		want []string
	}{
		{
			name: "magic link email",
			text: "Hi Chris,\n\nHere is your sign-in link:\n\nhttps://oink.mywire.org/sign-in/magic?token=abc123\n\nIt expires in 15 minutes.\n",
			want: []string{"https://oink.mywire.org/sign-in/magic?token=abc123"},
		},
		{
			name: "invite email",
			text: "Andreas has invited you to join Oentoro on Hearth.\n\nhttps://oink.mywire.org/invite/xyz789\n",
			want: []string{"https://oink.mywire.org/invite/xyz789"},
		},
		{
			name: "a full stop after a URL is not part of it",
			text: "Open https://oink.mywire.org/invite/xyz789.",
			want: []string{"https://oink.mywire.org/invite/xyz789"},
		},
		{
			name: "a token ending in a hyphen keeps its last character",
			text: "https://oink.mywire.org/invite/AbC-",
			want: []string{"https://oink.mywire.org/invite/AbC-"},
		},
		{
			name: "a token ending in an underscore keeps its last character",
			text: "https://oink.mywire.org/sign-in/magic?token=AbC_",
			want: []string{"https://oink.mywire.org/sign-in/magic?token=AbC_"},
		},
		{
			name: "the same URL twice appears once, in source order",
			text: "https://a.example/1 then https://b.example/2 then https://a.example/1",
			want: []string{"https://a.example/1", "https://b.example/2"},
		},
		{
			name: "a mailto is not a link the operator can hand over",
			text: "Reply to mailto:hearth@example.com or open https://a.example/1",
			want: []string{"https://a.example/1"},
		},
		{
			name: "html is read only when there is no text part, with entities unescaped",
			html: `<p>Open <a href="https://a.example/x?y=1&amp;z=2">your link</a></p>`,
			want: []string{"https://a.example/x?y=1&z=2"},
		},
		{
			name: "a text part wins over an html part",
			text: "https://text.example/1",
			html: `<a href="https://html.example/2">x</a>`,
			want: []string{"https://text.example/1"},
		},
		{
			name: "a message with no links is an empty slice, never nil",
			text: "Your household is ready.",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.ExtractLinks(tt.text, tt.html)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractLinks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
cd api && go test ./internal/domain/ -run TestExtractLinks -v
```

Expected: a compile failure, `undefined: domain.ExtractLinks`.

- [ ] **Step 3: Write the implementation**

Create `api/internal/domain/outbox_links.go`:

```go
package domain

import (
	"html"
	"regexp"
	"strings"
)

// linkPattern matches an http or https URL up to the first character that
// cannot appear inside one unescaped: whitespace, or one of the delimiters
// that surround a URL in HTML (<, >, ", '). Trailing sentence punctuation is
// removed afterwards rather than excluded here, because a "." or a "?" is
// legitimately *inside* most of Hearth's URLs and only ever junk at the end.
var linkPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// trailingPunctuation is stripped from the end of a match. It is written out
// character by character on purpose, and "-" and "_" are deliberately absent:
// every token in this product is base64.RawURLEncoding (adapter/crypto/
// tokens.go), whose alphabet includes both, and both link shapes put the
// token last -- "<base>/invite/<token>" and
// "<base>/sign-in/magic?token=<token>". Adding either character here would
// silently truncate roughly one token in thirty-two into a link that looks
// right, copies right, and fails on use. Do not "simplify" this into a
// general non-alphanumeric strip.
const trailingPunctuation = `.,;:!?)]>"'`

// ExtractLinks returns every http and https URL in a message body, in the
// order they appear, with duplicates removed. It reads text when text has
// content and falls back to htmlBody otherwise.
//
// Hearth sends text/plain only (adapter/mail/smtp.go builds every message
// with gomail.TypeTextPlain), so the HTML path is for messages this product
// did not send -- anything that speaks SMTP to Mailpit lands in the same
// store, and an empty link list on such a message would read as a broken
// screen rather than as a message with no links.
//
// The result is always non-nil, so a caller can range over it and a JSON
// encoder writes [] rather than null.
func ExtractLinks(text, htmlBody string) []string {
	source := text
	if strings.TrimSpace(source) == "" {
		// Only on this path: an href writes "&amp;" where the URL has "&".
		source = html.UnescapeString(htmlBody)
	}

	links := make([]string, 0)
	seen := make(map[string]bool)
	for _, match := range linkPattern.FindAllString(source, -1) {
		link := strings.TrimRight(match, trailingPunctuation)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		links = append(links, link)
	}
	return links
}
```

- [ ] **Step 4: Run the test and watch it pass**

```bash
cd api && go test ./internal/domain/ -run TestExtractLinks -v
```

Expected: PASS, ten subtests.

- [ ] **Step 5: Mutation-check the fixture that exists to catch a real mistake**

Temporarily add `-` to `trailingPunctuation`:

```go
const trailingPunctuation = `.,;:!?)]>"'-`
```

Re-run. Expected: **FAIL** on
`a_token_ending_in_a_hyphen_keeps_its_last_character` only. Restore the
constant and re-run to green. A test that stays green here is not protecting
the rule its comment claims to protect.

- [ ] **Step 6: Commit**

```bash
git add api/internal/domain/outbox_links.go api/internal/domain/outbox_links_test.go
git commit -m "feat(domain): extract the links out of a message body"
```

---

### Task 2: The `MailOutbox` port and `AdminOutboxService`

**Files:**
- Modify: `api/internal/usecase/ports.go` (append)
- Create: `api/internal/usecase/admin_outbox.go`
- Test: `api/internal/usecase/admin_outbox_test.go`

**Interfaces:**
- Consumes: `domain.ExtractLinks` (Task 1).
- Produces:
  - `usecase.MailOutbox` interface — `Recent(ctx, limit) (OutboxPage, error)`,
    `Message(ctx, id) (OutboxMessage, error)`. Task 3 implements it.
  - `usecase.OutboxPage{Messages []OutboxMessage; Total int}`
  - `usecase.OutboxMessage{ID, To, Subject string; SentAt time.Time; Text, HTML string}`
  - `usecase.ErrOutboxUnavailable`
  - `usecase.NewAdminOutboxService(MailOutbox) *AdminOutboxService`
  - `(*AdminOutboxService).List(ctx, limit) (OutboxListing, error)`
  - `(*AdminOutboxService).Message(ctx, id) (OutboxMessageView, error)`
  - `usecase.OutboxListing{Messages []OutboxMessage; Total int; Truncated bool}`
  - `usecase.OutboxMessageView{ID, To, Subject string; SentAt time.Time; Text string; Links []string}`
  - `usecase.OutboxDefaultLimit = 50`, `usecase.OutboxMaxLimit = 200`

- [ ] **Step 1: Write the failing test**

Create `api/internal/usecase/admin_outbox_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// fakeOutbox is the in-memory double every test here runs against. It
// records the limit it was asked for, which is how the clamping tests
// observe what the service decided.
type fakeOutbox struct {
	page       usecase.OutboxPage
	message    usecase.OutboxMessage
	err        error
	gotLimit   int
	gotMessage string
}

func (f *fakeOutbox) Recent(_ context.Context, limit int) (usecase.OutboxPage, error) {
	f.gotLimit = limit
	if f.err != nil {
		return usecase.OutboxPage{}, f.err
	}
	return f.page, nil
}

func (f *fakeOutbox) Message(_ context.Context, id string) (usecase.OutboxMessage, error) {
	f.gotMessage = id
	if f.err != nil {
		return usecase.OutboxMessage{}, f.err
	}
	return f.message, nil
}

func TestOutboxListClampsTheLimitAtBothEnds(t *testing.T) {
	for _, tt := range []struct {
		name string
		ask  int
		want int
	}{
		{"zero means the default", 0, usecase.OutboxDefaultLimit},
		{"negative means the default", -3, usecase.OutboxDefaultLimit},
		{"above the maximum is capped", usecase.OutboxMaxLimit + 1, usecase.OutboxMaxLimit},
		{"a usable number is honoured", 7, 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outbox := &fakeOutbox{}
			svc := usecase.NewAdminOutboxService(outbox)
			if _, err := svc.List(context.Background(), tt.ask); err != nil {
				t.Fatalf("List: %v", err)
			}
			if outbox.gotLimit != tt.want {
				t.Fatalf("limit asked of the outbox = %d, want %d", outbox.gotLimit, tt.want)
			}
		})
	}
}

func TestOutboxListReportsTruncatedWhenTheOutboxHoldsMore(t *testing.T) {
	outbox := &fakeOutbox{page: usecase.OutboxPage{
		Messages: []usecase.OutboxMessage{{ID: "a"}, {ID: "b"}},
		Total:    9,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	listing, err := svc.List(context.Background(), 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !listing.Truncated {
		t.Fatal("Truncated = false, want true: 9 held, 2 returned")
	}
	if listing.Total != 9 {
		t.Fatalf("Total = %d, want 9", listing.Total)
	}
}

func TestOutboxListIsNotTruncatedWhenEverythingFits(t *testing.T) {
	outbox := &fakeOutbox{page: usecase.OutboxPage{
		Messages: []usecase.OutboxMessage{{ID: "a"}, {ID: "b"}},
		Total:    2,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	listing, err := svc.List(context.Background(), 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Truncated {
		t.Fatal("Truncated = true, want false: 2 held, 2 returned")
	}
}

// The view type is where the HTML body stops. This test does not prove that
// on its own -- nothing here would fail if OutboxMessageView grew an HTML
// field, and claiming otherwise would be exactly the false confidence
// docs/LEARNING.md pattern 2 is about. What it does prove is that the links
// are extracted from the body rather than taken from anything the outbox
// handed over ready made. The field's absence is held one layer out, by Task
// 5's exact-key assertion and Task 6's .strict() schema.
func TestOutboxMessageReturnsExtractedLinksAndTheTextBody(t *testing.T) {
	outbox := &fakeOutbox{message: usecase.OutboxMessage{
		ID:      "0OQ1sV2mB7hN4kR8xT3wZq",
		To:      "chris@example.com",
		Subject: "Your Hearth sign-in link",
		SentAt:  time.Date(2026, 9, 4, 9, 12, 33, 0, time.UTC),
		Text:    "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
		HTML:    `<a href="https://never.example/rendered">x</a>`,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	view, err := svc.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if outbox.gotMessage != "0OQ1sV2mB7hN4kR8xT3wZq" {
		t.Fatalf("id asked of the outbox = %q", outbox.gotMessage)
	}
	if len(view.Links) != 1 || view.Links[0] != "https://oink.mywire.org/sign-in/magic?token=abc123" {
		t.Fatalf("Links = %#v", view.Links)
	}
	if view.Text != outbox.message.Text {
		t.Fatalf("Text = %q, want the body verbatim", view.Text)
	}
	if view.To != "chris@example.com" || view.Subject != "Your Hearth sign-in link" {
		t.Fatalf("view = %#v", view)
	}
}

// A text part exists on every Hearth message, so the HTML part must never be
// the source of the links a screen shows -- if it were, the rendered-HTML
// surface this design rejected would be one field away.
func TestOutboxMessageIgnoresTheHTMLPartWhenThereIsText(t *testing.T) {
	outbox := &fakeOutbox{message: usecase.OutboxMessage{
		Text: "Open https://text.example/1",
		HTML: `<a href="https://html.example/2">x</a>`,
	}}
	svc := usecase.NewAdminOutboxService(outbox)

	view, err := svc.Message(context.Background(), "id")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if len(view.Links) != 1 || view.Links[0] != "https://text.example/1" {
		t.Fatalf("Links = %#v, want the text part's URL only", view.Links)
	}
}

// Both failures travel unchanged: the HTTP layer answers 502 for one and 404
// for the other, and a service that flattened them into a single error would
// make those two answers impossible to tell apart.
func TestOutboxPassesTheOutboxsOwnFailuresThrough(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"unavailable", usecase.ErrOutboxUnavailable},
		{"not found", domain.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := usecase.NewAdminOutboxService(&fakeOutbox{err: tt.err})

			if _, err := svc.List(context.Background(), 0); !errors.Is(err, tt.err) {
				t.Fatalf("List error = %v, want %v", err, tt.err)
			}
			if _, err := svc.Message(context.Background(), "id"); !errors.Is(err, tt.err) {
				t.Fatalf("Message error = %v, want %v", err, tt.err)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/usecase/ -run TestOutbox -v
```

Expected: compile failure, `undefined: usecase.NewAdminOutboxService`.

- [ ] **Step 3: Add the port to `ports.go`**

Append to `api/internal/usecase/ports.go`:

```go
// ErrOutboxUnavailable means the outbox itself could not be read: it is
// unreachable, it timed out, or it answered something this code cannot map.
// It is declared here rather than in an adapter because it is part of the
// port's contract -- every implementation must be able to say "the store is
// there, I just could not reach it", and the HTTP layer answers 502 for it.
//
// It is deliberately distinct from domain.ErrNotFound, which means the outbox
// answered and does not hold that message. An operator needs different advice
// in each case: one is "Mailpit is down", the other is "that message has aged
// out of a store with no volume".
var ErrOutboxUnavailable = errors.New("the message outbox could not be read")

// MailOutbox reads messages the product has sent. It exists so the operator
// can hand someone a link that mail cannot deliver (see ADR 3). The only
// implementation today reads Mailpit; when mail leaves the box this port gets
// a second one instead of a rewrite.
//
// Message reports domain.ErrNotFound for a message the outbox does not hold.
// Both methods report ErrOutboxUnavailable when the outbox itself cannot be
// read.
//
// Neither method extracts anything: an implementation hands back the body
// parts exactly as the store gave them, and AdminOutboxService turns those
// into what a screen shows. That split is what keeps "which strings are
// links" testable without an HTTP server.
type MailOutbox interface {
	// Recent returns up to limit messages, newest first, with both body
	// fields left empty -- a list never carries a body, because a body can
	// contain a live single-use link and listing must not be the act that
	// reveals one.
	Recent(ctx context.Context, limit int) (OutboxPage, error)
	// Message returns one message including both body parts.
	Message(ctx context.Context, id string) (OutboxMessage, error)
}

// OutboxPage is one screenful of the outbox. Total is how many messages the
// outbox holds altogether, not how many were returned -- the screen says
// "showing 50 of 128", and a caller cannot infer that from a slice exactly as
// long as the limit it asked for.
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

If `ports.go` does not already import `errors` and `time`, add them.

- [ ] **Step 4: Write the service**

Create `api/internal/usecase/admin_outbox.go`:

```go
package usecase

import (
	"context"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AdminOutboxService is the operator's read of the mail this install has
// sent. It is its own service rather than three more methods on AdminService
// for the same reason AdminDirectoryService is: that service is "who is a
// platform admin, feature flags, the audit log", and this one reads a store
// outside the database entirely.
//
// It takes no actor parameter. The /admin guards in the HTTP layer are the
// only gate, as everywhere else in this product.
type AdminOutboxService struct{ outbox MailOutbox }

const (
	// OutboxDefaultLimit is how many messages the list returns when the
	// caller names no limit or an unusable one.
	OutboxDefaultLimit = 50
	// OutboxMaxLimit is the most the list will return. Its own constant
	// rather than the directory's: the two answer different questions, and
	// sharing one would make a change to either move the other.
	OutboxMaxLimit = 200
)

func NewAdminOutboxService(outbox MailOutbox) *AdminOutboxService {
	return &AdminOutboxService{outbox: outbox}
}

// OutboxListing is the list screen's whole answer.
type OutboxListing struct {
	Messages []OutboxMessage
	// Total is what the outbox holds; Truncated says it holds more than
	// this listing carries.
	Total     int
	Truncated bool
}

// OutboxMessageView is one message as the operator sees it: the links pulled
// out of whichever body part had them, and the plain text for context.
//
// There is deliberately no HTML field. Nothing above this service has a use
// for the HTML part, and the surface that would render it is the one this
// design rejected -- see the spec's decision 1. Adding the field back is the
// first step of building that surface by accident.
type OutboxMessageView struct {
	ID      string
	To      string
	Subject string
	SentAt  time.Time
	Text    string
	Links   []string
}

// List returns the newest messages the outbox holds, with the limit clamped.
// An unusable limit becomes the default rather than an error: the operator
// typed a URL, not a form.
func (s *AdminOutboxService) List(ctx context.Context, limit int) (OutboxListing, error) {
	if limit <= 0 {
		limit = OutboxDefaultLimit
	}
	if limit > OutboxMaxLimit {
		limit = OutboxMaxLimit
	}

	page, err := s.outbox.Recent(ctx, limit)
	if err != nil {
		return OutboxListing{}, err
	}
	return OutboxListing{
		Messages:  page.Messages,
		Total:     page.Total,
		Truncated: page.Total > len(page.Messages),
	}, nil
}

// Message returns one message with its links extracted. This is the only
// place domain.ExtractLinks is called, and the only place the HTML part is
// read -- it goes no further.
func (s *AdminOutboxService) Message(ctx context.Context, id string) (OutboxMessageView, error) {
	message, err := s.outbox.Message(ctx, id)
	if err != nil {
		return OutboxMessageView{}, err
	}
	return OutboxMessageView{
		ID:      message.ID,
		To:      message.To,
		Subject: message.Subject,
		SentAt:  message.SentAt,
		Text:    message.Text,
		Links:   domain.ExtractLinks(message.Text, message.HTML),
	}, nil
}
```

Add `"time"` to this file's imports if `OutboxMessageView` needs it — it
does, for `SentAt`.

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd api && go test ./internal/usecase/ -run TestOutbox -v
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the truncation rule**

Change `Truncated: page.Total > len(page.Messages)` to `Truncated: false`.
Re-run. Expected: **FAIL** in
`TestOutboxListReportsTruncatedWhenTheOutboxHoldsMore`. Restore and re-run to
green.

- [ ] **Step 7: Arch lint and commit**

```bash
cd .. && make lint-arch
git add api/internal/usecase/ports.go api/internal/usecase/admin_outbox.go api/internal/usecase/admin_outbox_test.go
git commit -m "feat(usecase): the MailOutbox port and the outbox service"
```

---

### Task 3: The Mailpit adapter

**Files:**
- Create: `api/internal/adapter/mail/mailpit_outbox.go`
- Test: `api/internal/adapter/mail/mailpit_outbox_test.go`

**Interfaces:**
- Consumes: `usecase.MailOutbox`, `usecase.OutboxPage`, `usecase.OutboxMessage`,
  `usecase.ErrOutboxUnavailable`, `domain.ErrNotFound` (Task 2).
- Produces: `mail.NewMailpitOutbox(baseURL string) *MailpitOutbox`, which
  satisfies `usecase.MailOutbox`. Task 5's `main.go` wiring is its only
  caller.

**Mailpit's API, at the pinned image `axllent/mailpit:v1.30.5`** — read off
the tag's own source, not from memory:

- `GET /api/v1/messages?limit=N` → `{"total": 12, "messages": [{"ID": "…",
  "To": [{"Name": "", "Address": "chris@example.com"}], "Subject": "…",
  "Created": "2026-09-04T09:12:33.123Z"}]}`, newest first.
- `GET /api/v1/message/{id}` → `{"ID": "…", "To": […], "Subject": "…",
  "Date": "…", "Text": "…", "HTML": "…"}`. **Marks the message read** in
  Mailpit — see the spec's decision 15.
- **`GET /api/v1/message/{id}/link-check` must never be called.** It issues a
  real HTTP request to every URL in the body. Spec decision 2.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/mail/mailpit_outbox_test.go`:

```go
package mail_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/mail"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// recordingMailpit is an httptest server that remembers every path it was
// asked for. The paths matter as much as the answers: see
// TestMailpitOutboxNeverCallsLinkCheck.
type recordingMailpit struct {
	mu     sync.Mutex
	paths  []string
	server *httptest.Server
}

func newRecordingMailpit(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *recordingMailpit {
	t.Helper()
	rec := &recordingMailpit{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		// EscapedPath, not Path: Go's server has already decoded Path by the
		// time a handler sees it, so a request for ".../..%2Fmessages"
		// arrives as ".../../messages" there and the escaping test below
		// would fail against correct code. EscapedPath is what actually went
		// over the wire.
		rec.paths = append(rec.paths, r.URL.EscapedPath())
		rec.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func (m *recordingMailpit) requestedPaths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.paths...)
}

// The JSON below is Mailpit v1.30.5's own shape, field for field.
const listJSON = `{
  "total": 9,
  "unread": 0,
  "messages_count": 9,
  "start": 0,
  "messages": [
    {"ID": "0OQ1sV2mB7hN4kR8xT3wZq",
     "To": [{"Name": "", "Address": "chris@example.com"}],
     "Subject": "Your Hearth sign-in link",
     "Created": "2026-09-04T09:12:33.123456Z",
     "Snippet": "Here is your sign-in link: https://oink.mywire.org/sign-in/magic?token=abc123"}
  ]
}`

const messageJSON = `{
  "ID": "0OQ1sV2mB7hN4kR8xT3wZq",
  "To": [{"Name": "", "Address": "chris@example.com"}],
  "Subject": "Your Hearth sign-in link",
  "Date": "2026-09-04T09:12:30Z",
  "Text": "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
  "HTML": ""
}`

func TestMailpitOutboxRecentMapsTheListResponse(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "50" {
			t.Errorf("limit query = %q, want 50", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	page, err := outbox.Recent(context.Background(), 50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if page.Total != 9 {
		t.Fatalf("Total = %d, want 9", page.Total)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(page.Messages))
	}
	got := page.Messages[0]
	if got.ID != "0OQ1sV2mB7hN4kR8xT3wZq" || got.To != "chris@example.com" {
		t.Fatalf("message = %#v", got)
	}
	if got.SentAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-09-04T09:12:33Z" {
		t.Fatalf("SentAt = %v, want Mailpit's Created", got.SentAt)
	}
	// A list must never carry a body, whatever Mailpit offered: the snippet
	// above contains a whole working link.
	if got.Text != "" || got.HTML != "" {
		t.Fatalf("a listed message carried a body: %#v", got)
	}
	if paths := server.requestedPaths(); len(paths) != 1 || paths[0] != "/api/v1/messages" {
		t.Fatalf("requested %v", paths)
	}
}

func TestMailpitOutboxMessageMapsTheDetailResponse(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(messageJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	message, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if message.Text == "" {
		t.Fatal("Text is empty, want the body")
	}
	if message.To != "chris@example.com" {
		t.Fatalf("To = %q", message.To)
	}
	if paths := server.requestedPaths(); len(paths) != 1 ||
		paths[0] != "/api/v1/message/0OQ1sV2mB7hN4kR8xT3wZq" {
		t.Fatalf("requested %v", paths)
	}
}

// Decision 2 of the spec, held by a test rather than by a comment. Mailpit's
// link-check endpoint issues a real HTTP request to every URL in the body,
// and every URL in a Hearth email is a live single-use token on a public
// host. This is the kind of rule that survives review and dies in a later
// refactor unless something fails when it is broken.
func TestMailpitOutboxNeverCallsLinkCheck(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(messageJSON))
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	if _, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq"); err != nil {
		t.Fatalf("Message: %v", err)
	}
	for _, path := range server.requestedPaths() {
		if path != "/api/v1/message/0OQ1sV2mB7hN4kR8xT3wZq" {
			t.Fatalf("requested an unexpected upstream path %q", path)
		}
	}
}

func TestMailpitOutboxMapsAMissingMessageToNotFound(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	_, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want domain.ErrNotFound", err)
	}
}

func TestMailpitOutboxMapsEveryOtherFailureToUnavailable(t *testing.T) {
	t.Run("a non-2xx that is not a 404", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>not json</html>"))
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("a message with no recipient at all", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ID":"0OQ1sV2mB7hN4kR8xT3wZq","To":[],"Subject":"x","Date":"2026-09-04T09:12:30Z","Text":"y","HTML":""}`))
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Message(context.Background(), "0OQ1sV2mB7hN4kR8xT3wZq"); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	// A list has nothing to not-find. Mailpit supports a configured webroot,
	// so a MAILPIT_API_URL carrying a stray path segment makes
	// /api/v1/messages answer 404 -- and mapping that to ErrNotFound would
	// reach the screen as an empty list under a line that says messages do
	// not last, which is both wrong and convincing.
	t.Run("a 404 on the list is unavailable, not not-found", func(t *testing.T) {
		server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		outbox := mail.NewMailpitOutbox(server.server.URL)
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})

	t.Run("nothing listening", func(t *testing.T) {
		// A port nothing is bound to: the transport itself fails.
		outbox := mail.NewMailpitOutbox("http://127.0.0.1:1")
		if _, err := outbox.Recent(context.Background(), 10); !errors.Is(err, usecase.ErrOutboxUnavailable) {
			t.Fatalf("error = %v, want ErrOutboxUnavailable", err)
		}
	})
}

// The id reaches the adapter already validated by the handler, but the
// adapter escapes it anyway: the check and the URL construction live in
// different files, and only one of them is in front of someone making a
// change.
func TestMailpitOutboxEscapesTheMessageID(t *testing.T) {
	server := newRecordingMailpit(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	outbox := mail.NewMailpitOutbox(server.server.URL)

	_, _ = outbox.Message(context.Background(), "../messages")

	paths := server.requestedPaths()
	if len(paths) != 1 || paths[0] != "/api/v1/message/..%2Fmessages" {
		t.Fatalf("requested %v, want the escaped segment", paths)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/mail/ -run TestMailpitOutbox -v
```

Expected: compile failure, `undefined: mail.NewMailpitOutbox`.

- [ ] **Step 3: Write the adapter**

Create `api/internal/adapter/mail/mailpit_outbox.go` — the package is `mail`,
the same package `smtp.go` is in (checked, not assumed):

```go
package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// MailpitOutbox reads the messages Mailpit has caught. It is the only
// implementation of usecase.MailOutbox, and it is a plain JSON client on
// purpose: everything interesting about a message -- which of its strings
// are links -- is domain.ExtractLinks' job, one layer in.
//
// Two Mailpit endpoints are used and no others:
//
//	GET /api/v1/messages?limit=N   the list, newest first
//	GET /api/v1/message/{id}       one message, both body parts
//
// GET /api/v1/message/{id}/link-check is deliberately NOT used, and a test
// asserts that no other path is ever requested. It issues a real HTTP request
// to every URL it finds in order to report each one's status, and every URL
// in a Hearth email is a live single-use token on a public host.
//
// Reading a message marks it read in Mailpit's own store. That is a write,
// from a panel described as read-only, and it is accepted: the flag is not
// product state and nothing in Hearth reads it. Avoiding it would mean
// fetching the raw source and parsing MIME here.
type MailpitOutbox struct {
	base string
	http *http.Client
}

// NewMailpitOutbox points at Mailpit's HTTP API -- http://mailpit:8025 in
// both Compose stacks.
//
// The timeout is short because Mailpit is a container on the same host: a
// slow answer means something is wrong, not that something is far away, and
// an operator is better served by a prompt 502 than by a page that hangs.
func NewMailpitOutbox(baseURL string) *MailpitOutbox {
	return &MailpitOutbox{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// mailpitAddress mirrors net/mail.Address as Mailpit serialises it.
type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type mailpitSummary struct {
	ID      string           `json:"ID"`
	To      []mailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	// Created is when Mailpit received the message. The list response has no
	// Date field at all, which is why SentAt comes from a different source on
	// each route -- see Message below.
	Created time.Time `json:"Created"`
}

type mailpitList struct {
	Total    int              `json:"total"`
	Messages []mailpitSummary `json:"messages"`
}

type mailpitMessage struct {
	ID      string           `json:"ID"`
	To      []mailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	// Date is the message's own header, falling back to the received time.
	// The detail response has no Created field, so this is the only sent-at
	// Mailpit offers here. It can differ from the list's Created by the
	// length of the SMTP hop, which is to a container on the same host and
	// which Hearth's own client stamps at send -- so in this install the two
	// agree, and a one-second difference between the two screens is the hop
	// rather than a bug.
	Date time.Time `json:"Date"`
	Text string    `json:"Text"`
	HTML string    `json:"HTML"`
}

// firstRecipient fails closed. Hearth addresses every message to exactly one
// person (adapter/mail/smtp.go), so an empty list means the assumption this
// mapping rests on has changed -- better a 502 the operator can report than a
// blank cell nobody notices.
func firstRecipient(addresses []mailpitAddress) (string, error) {
	if len(addresses) == 0 || addresses[0].Address == "" {
		return "", fmt.Errorf("%w: a message with no recipient", usecase.ErrOutboxUnavailable)
	}
	return addresses[0].Address, nil
}

// errUpstreamNotFound is get's own signal that the upstream answered 404. It
// is not domain.ErrNotFound: what a 404 MEANS depends on which route asked.
// On the message route it is "Mailpit no longer holds that message"; on the
// list route there is nothing to not-find, and a 404 there means the base URL
// is wrong -- Mailpit supports a configured webroot, so a stray path segment
// in MAILPIT_API_URL produces exactly that. Each caller translates it.
var errUpstreamNotFound = errors.New("mailpit answered 404")

// get performs one upstream request and decodes its body. A 404 becomes
// errUpstreamNotFound for the caller to interpret; every other failure --
// transport, status, body -- becomes usecase.ErrOutboxUnavailable, because
// from a caller's point of view they are the same event: the outbox is there
// and could not be read.
func (m *MailpitOutbox) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", usecase.ErrOutboxUnavailable, err)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", usecase.ErrOutboxUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return errUpstreamNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: mailpit answered %d", usecase.ErrOutboxUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode response: %v", usecase.ErrOutboxUnavailable, err)
	}
	return nil
}

func (m *MailpitOutbox) Recent(ctx context.Context, limit int) (usecase.OutboxPage, error) {
	var list mailpitList
	if err := m.get(ctx, "/api/v1/messages?limit="+strconv.Itoa(limit), &list); err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			// See errUpstreamNotFound: on the list this is a wrong base URL,
			// not a missing message, and it must never reach the screen as an
			// empty list.
			return usecase.OutboxPage{}, fmt.Errorf("%w: mailpit answered 404 for the message list; check MAILPIT_API_URL", usecase.ErrOutboxUnavailable)
		}
		return usecase.OutboxPage{}, err
	}

	page := usecase.OutboxPage{
		Messages: make([]usecase.OutboxMessage, 0, len(list.Messages)),
		Total:    list.Total,
	}
	for _, summary := range list.Messages {
		to, err := firstRecipient(summary.To)
		if err != nil {
			return usecase.OutboxPage{}, err
		}
		// Text and HTML stay zero here, deliberately: Mailpit's summary
		// carries a Snippet of up to 250 characters of body, which for a
		// Hearth email is long enough to contain the whole link.
		page.Messages = append(page.Messages, usecase.OutboxMessage{
			ID:      summary.ID,
			To:      to,
			Subject: summary.Subject,
			SentAt:  summary.Created,
		})
	}
	return page, nil
}

func (m *MailpitOutbox) Message(ctx context.Context, id string) (usecase.OutboxMessage, error) {
	var message mailpitMessage
	path := "/api/v1/message/" + url.PathEscape(id)
	if err := m.get(ctx, path, &message); err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			// Here a 404 is the ordinary case the screen has copy for:
			// Mailpit's store has no volume, so a message can simply be gone.
			return usecase.OutboxMessage{}, domain.ErrNotFound
		}
		return usecase.OutboxMessage{}, err
	}

	to, err := firstRecipient(message.To)
	if err != nil {
		return usecase.OutboxMessage{}, err
	}
	return usecase.OutboxMessage{
		ID:      message.ID,
		To:      to,
		Subject: message.Subject,
		SentAt:  message.Date,
		Text:    message.Text,
		HTML:    message.HTML,
	}, nil
}
```

- [ ] **Step 4: Assert the adapter satisfies the port**

Add to `api/cmd/api/main.go`, beside the existing
`var _ telegram.StartHandler = …` line (this is a compile-time check, not
runtime code):

```go
var _ usecase.MailOutbox = (*mail.MailpitOutbox)(nil)
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/mail/ -run TestMailpitOutbox -v
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the two rules that matter most**

1. Make the list carry a body — in `Recent`, add
   `Text: summary.Subject` to the appended `usecase.OutboxMessage`. Re-run.
   Expected: **FAIL** in `TestMailpitOutboxRecentMapsTheListResponse`.
2. Drop the escaping — change `url.PathEscape(id)` to `id`. Re-run.
   Expected: **FAIL** in `TestMailpitOutboxEscapesTheMessageID`.

Restore both and re-run to green.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/mail/mailpit_outbox.go api/internal/adapter/mail/mailpit_outbox_test.go api/cmd/api/main.go
git commit -m "feat(mail): read Mailpit's HTTP API through the MailOutbox port"
```

---

### Task 4: `MAILPIT_API_URL`

**Files:**
- Modify: `api/internal/config/config.go`
- Test: `api/internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config.MailpitAPIURL string` and
  `(config.Config).OutboxEnabled() bool`. Task 5's `main.go` wiring uses both.

- [ ] **Step 1: Write the failing test**

Append to `api/internal/config/config_test.go` (follow the file's existing
helper for setting environment variables — if it sets them with `t.Setenv`,
do the same):

```go
func TestLoadAcceptsAnAbsentMailpitAPIURL(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MailpitAPIURL != "" {
		t.Fatalf("MailpitAPIURL = %q, want empty", cfg.MailpitAPIURL)
	}
	if cfg.OutboxEnabled() {
		t.Fatal("OutboxEnabled() = true with no URL set")
	}
}

func TestLoadAcceptsAMailpitAPIURL(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("MAILPIT_API_URL", "http://mailpit:8025")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.OutboxEnabled() {
		t.Fatal("OutboxEnabled() = false with a URL set")
	}
}

// A typo in .env must stop the boot, not present on the box as a 502 with
// nothing pointing at the line that caused it -- docs/LEARNING.md pattern 8,
// "configuration that lies".
func TestLoadRefusesAnUnusableMailpitAPIURL(t *testing.T) {
	for _, value := range []string{"mailpit:8025", "://mailpit", "ftp://mailpit:8025"} {
		t.Run(value, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv("MAILPIT_API_URL", value)

			if _, err := config.Load(); err == nil {
				t.Fatalf("Load accepted MAILPIT_API_URL=%q", value)
			}
		})
	}
}
```

If the file has no `setMinimalEnv` helper, use whatever the existing tests in
that file use to build a loadable environment — read them first and match.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/config/ -run TestLoad -v
```

Expected: FAIL, `cfg.MailpitAPIURL undefined`.

- [ ] **Step 3: Add the field**

In `api/internal/config/config.go`, add to the `Config` struct beside the
Telegram pair:

```go
	// MailpitAPIURL is Mailpit's HTTP API, http://mailpit:8025 in both
	// Compose stacks. Optional: empty means the operator's outbound message
	// inspector is unavailable and says so, rather than showing an empty
	// list -- an empty list would read as "Hearth has sent no mail".
	//
	// A value that is set but unusable refuses the boot, for the same reason
	// the SMTP and Telegram pairs do: a typo here would otherwise present on
	// the box as a 502 on one admin screen, with nothing pointing back at the
	// .env line that caused it.
	MailpitAPIURL string
```

Add the accessor beside `TelegramEnabled`:

```go
// OutboxEnabled reports whether the outbound message inspector is configured.
// When it is false the admin routes answer 503 and say which variable is
// missing -- never 404, because everyone who can reach them has already
// proved they are a platform admin with a live grant, and hiding the route
// from them would cost them the one fact that tells them what to fix.
func (c Config) OutboxEnabled() bool { return c.MailpitAPIURL != "" }
```

Read it in `Load`, beside the Telegram variables:

```go
		MailpitAPIURL: os.Getenv("MAILPIT_API_URL"),
```

And validate it, after the Telegram check:

```go
	if cfg.MailpitAPIURL != "" {
		parsed, err := url.Parse(cfg.MailpitAPIURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, fmt.Errorf(`MAILPIT_API_URL must be an http or https URL, got %q`, cfg.MailpitAPIURL)
		}
	}
```

Add `"net/url"` to the imports.

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd api && go test ./internal/config/ -run TestLoad -v
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the refusal**

Delete the `parsed.Scheme` half of the condition. Re-run. Expected: **FAIL**
on the `ftp://mailpit:8025` subtest. Restore and re-run to green.

- [ ] **Step 6: Commit**

```bash
git add api/internal/config/config.go api/internal/config/config_test.go
git commit -m "feat(config): MAILPIT_API_URL, refused at boot when unusable"
```

---

### Task 5: The two routes

The task that makes the feature real end to end: DTOs, handlers, the error
mapping, the route registration, the `main.go` wiring and the route-level
tests. It is one task because none of those pieces is independently
reviewable — a handler with no route is not a deliverable.

**Files:**
- Create: `api/internal/adapter/http/admin_outbox_handlers.go`
- Test: `api/internal/adapter/http/admin_outbox_api_test.go`
- Modify: `api/internal/adapter/http/router.go`, `api/internal/adapter/http/errors.go`,
  `api/internal/adapter/http/api_test.go`, `api/cmd/api/main.go`

**Interfaces:**
- Consumes: `usecase.AdminOutboxService` (Task 2),
  `mail.NewMailpitOutbox` (Task 3), `config.OutboxEnabled` (Task 4).
- Produces: `GET /api/v1/admin/mail` and `GET /api/v1/admin/mail/{messageID}`;
  `httpadapter.Deps.AdminOutbox *usecase.AdminOutboxService`;
  `newTestEnvWithOutbox` for later tests. Task 6's Zod schemas mirror the
  DTOs below exactly.

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/http/admin_outbox_api_test.go`:

```go
package httpadapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// stubOutbox is the MailOutbox a configured test env holds. Each test sets
// only the field it cares about.
type stubOutbox struct {
	page    usecase.OutboxPage
	message usecase.OutboxMessage
	err     error
}

func (s *stubOutbox) Recent(context.Context, int) (usecase.OutboxPage, error) {
	if s.err != nil {
		return usecase.OutboxPage{}, s.err
	}
	return s.page, nil
}

func (s *stubOutbox) Message(context.Context, string) (usecase.OutboxMessage, error) {
	if s.err != nil {
		return usecase.OutboxMessage{}, s.err
	}
	return s.message, nil
}

func sampleOutbox() *stubOutbox {
	sent := time.Date(2026, 9, 4, 9, 12, 33, 0, time.UTC)
	return &stubOutbox{
		page: usecase.OutboxPage{
			Total: 9,
			Messages: []usecase.OutboxMessage{{
				ID: "0OQ1sV2mB7hN4kR8xT3wZq", To: "chris@example.com",
				Subject: "Your Hearth sign-in link", SentAt: sent,
			}},
		},
		message: usecase.OutboxMessage{
			ID: "0OQ1sV2mB7hN4kR8xT3wZq", To: "chris@example.com",
			Subject: "Your Hearth sign-in link", SentAt: sent,
			Text: "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in.",
			HTML: `<a href="https://never.example/rendered">x</a>`,
		},
	}
}

// The key sets are asserted exactly, the same way the households tests
// assert that no money reaches that screen: here the property is that no
// body text reaches the list, and a field added to the DTO by accident must
// fail here rather than pass through.
func TestAdminMailListsMessagesWithExactlyTheSpecsKeys(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "top level", rec.Body.Bytes(), "messages", "total", "truncated")

	var body struct {
		Messages  []json.RawMessage `json:"messages"`
		Total     int               `json:"total"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(body.Messages))
	}
	assertKeys(t, "message", body.Messages[0], "id", "to", "subject", "sentAt")
	if body.Total != 9 || !body.Truncated {
		t.Fatalf("total = %d, truncated = %v", body.Total, body.Truncated)
	}
}

func TestAdminMailMessageReturnsLinksAndTextButNeverHTML(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertKeys(t, "message", rec.Body.Bytes(), "id", "to", "subject", "sentAt", "links", "text")

	var body struct {
		Links []string `json:"links"`
		Text  string   `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Links) != 1 || body.Links[0] != "https://oink.mywire.org/sign-in/magic?token=abc123" {
		t.Fatalf("links = %#v", body.Links)
	}
	if body.Text == "" {
		t.Fatal("text is empty")
	}
}

// Unconfigured is not the same event as unreachable, and neither is a 404:
// everyone who reaches these handlers has already proved they are a platform
// admin with a live grant.
func TestAdminMailSaysWhenItIsNotConfigured(t *testing.T) {
	env := newTestEnv(t) // no outbox: Deps.AdminOutbox is nil
	session := grantedAdmin(t, env)

	for _, path := range []string{"/api/v1/admin/mail", "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq"} {
		rec := env.authedGet(t, path, session)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, rec.Code)
		}
		if code := errorCode(t, rec.Body.Bytes()); code != "MAIL_INSPECTOR_NOT_CONFIGURED" {
			t.Fatalf("%s code = %q", path, code)
		}
	}
}

func TestAdminMailSaysWhenTheOutboxCannotBeRead(t *testing.T) {
	env := newTestEnvWithOutbox(t, &stubOutbox{err: usecase.ErrOutboxUnavailable})
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail", session)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if code := errorCode(t, rec.Body.Bytes()); code != "MAIL_UPSTREAM_UNAVAILABLE" {
		t.Fatalf("code = %q", code)
	}
}

func TestAdminMailMessageIsANotFoundWhenMailpitHasDroppedIt(t *testing.T) {
	env := newTestEnvWithOutbox(t, &stubOutbox{err: domain.ErrNotFound})
	session := grantedAdmin(t, env)

	rec := env.authedGet(t, "/api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq", session)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Fail closed on a value we did not construct. Mailpit ids are exactly 22
// characters of [0-9A-Za-z]; "latest" is Mailpit's own magic id for "the most
// recent message", so a typo must never reach the upstream request.
func TestAdminMailMessageRefusesAnIDThatIsNotMailpitShaped(t *testing.T) {
	env := newTestEnvWithOutbox(t, sampleOutbox())
	session := grantedAdmin(t, env)

	for _, id := range []string{
		"latest",
		"0OQ1sV2mB7hN4kR8xT3wZ",   // 21
		"0OQ1sV2mB7hN4kR8xT3wZqq", // 23
		"0OQ1sV2mB7hN4kR8xT3w-q",  // a character outside the alphabet
	} {
		rec := env.authedGet(t, "/api/v1/admin/mail/"+id, session)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q status = %d, want 400", id, rec.Code)
		}
		if code := errorCode(t, rec.Body.Bytes()); code != "INVALID_ID" {
			t.Fatalf("id %q code = %q", id, code)
		}
	}
}
```

If the package has no `errorCode` helper, add one to this file:

```go
func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return envelope.Error.Code
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run TestAdminMail -v
```

Expected: compile failure, `undefined: newTestEnvWithOutbox`.

- [ ] **Step 3: Let a test env carry an outbox**

In `api/internal/adapter/http/api_test.go`, rename the existing builder and
add two wrappers, so all three constructors share one body:

```go
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, clock.System{}, nil)
}

func newTestEnvWithClock(t *testing.T, clk usecase.Clock) *testEnv {
	t.Helper()
	return newTestEnvWith(t, clk, nil)
}

// newTestEnvWithOutbox builds an env whose admin surface has a configured
// message inspector. Passing nil (what the two constructors above do) is the
// unconfigured install, which is the state most of this package's tests want
// and the state a real install has until MAILPIT_API_URL is set.
func newTestEnvWithOutbox(t *testing.T, outbox usecase.MailOutbox) *testEnv {
	t.Helper()
	return newTestEnvWith(t, clock.System{}, outbox)
}

func newTestEnvWith(t *testing.T, clk usecase.Clock, outbox usecase.MailOutbox) *testEnv {
	// ... the existing body of newTestEnvWithClock, unchanged ...
}
```

Inside that shared body, build the service only when an outbox was supplied,
and add it to `Deps`:

```go
	var adminOutboxSvc *usecase.AdminOutboxService
	if outbox != nil {
		adminOutboxSvc = usecase.NewAdminOutboxService(outbox)
	}
```

```go
		AdminDirectory: adminDirectorySvc,
		AdminOutbox:    adminOutboxSvc,
```

- [ ] **Step 4: Add the `Deps` field and the routes**

In `api/internal/adapter/http/router.go`, beside `AdminDirectory`:

```go
	// AdminOutbox is nil when MAILPIT_API_URL is unset, the same shape
	// Telegram above has: the routes are registered either way so the tree
	// does not change with configuration, and the handlers' nil check is the
	// one place that decision lives. Unlike Telegram's 404, an unconfigured
	// outbox answers 503 and names the variable -- see the handlers.
	AdminOutbox *usecase.AdminOutboxService
```

And in the `granted` group, after the two households routes:

```go
					// The operator's outbound mail: two reads. See
					// admin_outbox_handlers.go.
					granted.Get("/mail", handleAdminMail(deps))
					granted.Get("/mail/{messageID}", handleAdminMailMessage(deps))
```

- [ ] **Step 5: Map the new sentinel**

In `api/internal/adapter/http/errors.go`, add a case to `MapDomainError`
beside the other `usecase.` sentinels:

```go
	case errors.Is(err, usecase.ErrOutboxUnavailable):
		// 502 rather than 500: the failure is upstream of this service and
		// the operator's next step is to look at Mailpit, not at the API's
		// own logs. Deliberately distinct from the 503 an unconfigured
		// inspector answers -- one means "set the variable", the other means
		// "the container is down", and collapsing them would send the
		// operator to fix the wrong thing.
		WriteError(w, http.StatusBadGateway, "MAIL_UPSTREAM_UNAVAILABLE",
			"Mailpit is not answering. The messages are not lost — the reader is.", nil)
```

- [ ] **Step 6: Write the handlers**

Create `api/internal/adapter/http/admin_outbox_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// The operator's outbound mail: a list and one message. Both are reads inside
// the /admin granted group, so requirePlatformAdmin, auditAdmin, requireCSRF
// and requireAdminGrant apply by construction -- nothing here checks who is
// asking. Every timestamp leaves as RFC 3339 in UTC.
//
// The list carries no body text of any kind, and the detail carries the plain
// text and the links pulled out of it, never the HTML part. A rendered email
// is not what this screen is for; see the spec's decision 1.

// mailpitIDPattern is Mailpit v1.30.5's id shape exactly: 22 characters from
// a 62-character alphanumeric alphabet (internal/shortuuid). Refusing
// anything else before the upstream request is made is this route's "fail
// closed on values you did not construct" -- with two specific teeth, since
// Mailpit reads the literal id "latest" as "the most recent message", and an
// id containing a slash would aim the request at a different endpoint
// entirely.
var mailpitIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

type outboxMessageSummaryDTO struct {
	ID      string    `json:"id"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	SentAt  time.Time `json:"sentAt"`
}

type outboxListResponse struct {
	Messages  []outboxMessageSummaryDTO `json:"messages"`
	Total     int                       `json:"total"`
	Truncated bool                      `json:"truncated"`
}

type outboxMessageResponse struct {
	ID      string    `json:"id"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	SentAt  time.Time `json:"sentAt"`
	Links   []string  `json:"links"`
	Text    string    `json:"text"`
}

// writeOutboxUnconfigured is the answer when MAILPIT_API_URL is unset. It is
// 503 and not 404 on purpose: a 404 here would hide the route from the one
// person allowed to use it, and the message names the variable because the
// person reading it is the person who can set it.
func writeOutboxUnconfigured(w http.ResponseWriter) {
	WriteError(w, http.StatusServiceUnavailable, "MAIL_INSPECTOR_NOT_CONFIGURED",
		"The message inspector is not configured on this install. Set MAILPIT_API_URL and restart the API.", nil)
}

// handleAdminMail is the list. A limit that fails to parse is 0, which the
// service turns into its default -- the operator typed a URL, not a form.
func handleAdminMail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminOutbox == nil {
			writeOutboxUnconfigured(w)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		listing, err := deps.AdminOutbox.List(r.Context(), limit)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := outboxListResponse{
			Messages:  make([]outboxMessageSummaryDTO, 0, len(listing.Messages)),
			Total:     listing.Total,
			Truncated: listing.Truncated,
		}
		for _, m := range listing.Messages {
			body.Messages = append(body.Messages, outboxMessageSummaryDTO{
				ID: m.ID, To: m.To, Subject: m.Subject, SentAt: m.SentAt.UTC(),
			})
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

// handleAdminMailMessage is the deliberate second click: opening one message
// is its own request and its own audit row, which is what makes seeing a live
// link an act with a record rather than the default state of a screen.
func handleAdminMailMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AdminOutbox == nil {
			writeOutboxUnconfigured(w)
			return
		}
		id := chi.URLParam(r, "messageID")
		if !mailpitIDPattern.MatchString(id) {
			WriteError(w, http.StatusBadRequest, "INVALID_ID", "That is not a message id.", nil)
			return
		}
		view, err := deps.AdminOutbox.Message(r.Context(), id)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, outboxMessageResponse{
			ID: view.ID, To: view.To, Subject: view.Subject,
			SentAt: view.SentAt.UTC(), Links: view.Links, Text: view.Text,
		})
	}
}
```

- [ ] **Step 7: Wire it in `main.go`**

In `api/cmd/api/main.go`, beside the Telegram block:

```go
	// Nil unless MAILPIT_API_URL is set. httpadapter.Deps.AdminOutbox being
	// nil is what makes the two /admin/mail routes answer 503 and name the
	// variable; the routes themselves are registered either way.
	var adminOutboxSvc *usecase.AdminOutboxService
	if cfg.OutboxEnabled() {
		adminOutboxSvc = usecase.NewAdminOutboxService(mail.NewMailpitOutbox(cfg.MailpitAPIURL))
	}
```

Add it to the `Deps` literal:

```go
			AdminDirectory: adminDirectorySvc,
			AdminOutbox:    adminOutboxSvc,
```

And log it at startup beside the existing Telegram line, so an operator can
see from the logs which optional features this process has:

```go
	if cfg.OutboxEnabled() {
		slog.Info("outbound message inspector enabled", "mailpit_api_url", cfg.MailpitAPIURL)
	}
```

- [ ] **Step 8: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -run TestAdminMail -v
cd api && go test ./... -count=1 -timeout=5m
```

Expected: PASS, and the whole package still green — Step 3 touched a
constructor every test in the package uses.

- [ ] **Step 9: Mutation-check two guards**

1. Delete the `deps.AdminOutbox == nil` check in `handleAdminMail`. Re-run.
   Expected: **FAIL** (a panic recovered into a 500) in
   `TestAdminMailSaysWhenItIsNotConfigured`.
2. Change `mailpitIDPattern` to `^.+$`. Re-run. Expected: **FAIL** in
   `TestAdminMailMessageRefusesAnIDThatIsNotMailpitShaped`.

Restore both and re-run to green.

- [ ] **Step 10: Commit**

```bash
cd .. && make lint
git add api/internal/adapter/http/ api/cmd/api/main.go
git commit -m "feat(api): GET /admin/mail and /admin/mail/{id}"
```

---

### Task 6: The frontend schemas and hooks

**Files:**
- Create: `web/src/features/admin/adminOutboxSchemas.ts`
- Create: `web/src/features/admin/useAdminOutbox.ts`
- Test: `web/src/features/admin/adminOutboxSchemas.test.ts`

**Interfaces:**
- Consumes: the DTOs from Task 5, field for field.
- Produces: `adminMailListSchema`, `adminMailMessageSchema`,
  `type AdminMailList`, `type AdminMailMessage`, `useAdminMail(limit)`,
  `useAdminMailMessage(id)`, `adminMailKey`, `adminMailMessageKey`,
  `OUTBOX_DEFAULT_LIMIT`, `OUTBOX_MAX_LIMIT`. Task 7's page consumes all of
  these.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/admin/adminOutboxSchemas.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  adminMailListSchema,
  adminMailMessageSchema,
} from "./adminOutboxSchemas";

describe("the outbox schemas", () => {
  it("parses the list shape the backend sends", () => {
    const parsed = adminMailListSchema.parse({
      messages: [
        {
          id: "0OQ1sV2mB7hN4kR8xT3wZq",
          to: "chris@example.com",
          subject: "Your Hearth sign-in link",
          sentAt: "2026-09-04T09:12:33Z",
        },
      ],
      total: 9,
      truncated: true,
    });
    expect(parsed.messages[0].to).toBe("chris@example.com");
  });

  // .strict() everywhere, for the same reason the directory schemas are:
  // a key the backend did not promise must fail the parse rather than reach
  // a screen. Here the key that must never appear is a body on a list row.
  it("refuses a listed message that carries a body", () => {
    expect(() =>
      adminMailListSchema.parse({
        messages: [
          {
            id: "0OQ1sV2mB7hN4kR8xT3wZq",
            to: "chris@example.com",
            subject: "Your Hearth sign-in link",
            sentAt: "2026-09-04T09:12:33Z",
            text: "Open https://oink.mywire.org/sign-in/magic?token=abc123",
          },
        ],
        total: 1,
        truncated: false,
      }),
    ).toThrow();
  });

  it("refuses a message that carries an html part", () => {
    expect(() =>
      adminMailMessageSchema.parse({
        id: "0OQ1sV2mB7hN4kR8xT3wZq",
        to: "chris@example.com",
        subject: "Your Hearth sign-in link",
        sentAt: "2026-09-04T09:12:33Z",
        links: [],
        text: "",
        html: "<p>never</p>",
      }),
    ).toThrow();
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/admin/adminOutboxSchemas.test.ts
```

Expected: FAIL, cannot resolve `./adminOutboxSchemas`.

- [ ] **Step 3: Write the schemas**

Create `web/src/features/admin/adminOutboxSchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/
// admin_outbox_handlers.go -- the adminDirectorySchemas.ts convention:
// follow the backend's own structs, not a guess at the shape.
//
// Every object is .strict(), and here that is load-bearing rather than
// tidy: the list must never carry body text (a Hearth email is short enough
// that a snippet can contain the whole link), and a message must never carry
// an HTML part. A backend change that added either would fail the parse
// instead of quietly reaching a screen.
import { z } from "zod";

export const adminMailSummarySchema = z
  .object({
    id: z.string(),
    to: z.string(),
    subject: z.string(),
    sentAt: z.string(),
  })
  .strict();
export type AdminMailSummary = z.infer<typeof adminMailSummarySchema>;

export const adminMailListSchema = z
  .object({
    messages: z.array(adminMailSummarySchema),
    total: z.number().int(),
    truncated: z.boolean(),
  })
  .strict();
export type AdminMailList = z.infer<typeof adminMailListSchema>;

export const adminMailMessageSchema = z
  .object({
    id: z.string(),
    to: z.string(),
    subject: z.string(),
    sentAt: z.string(),
    links: z.array(z.string()),
    text: z.string(),
  })
  .strict();
export type AdminMailMessage = z.infer<typeof adminMailMessageSchema>;
```

- [ ] **Step 4: Write the hooks**

Create `web/src/features/admin/useAdminOutbox.ts`:

```ts
// Query hooks over the outbox routes (api/internal/adapter/http/
// admin_outbox_handlers.go). Same shape as useAdminDirectory.ts, same two
// rules: refetchOnWindowFocus is off because every request under /admin is an
// audit row, and a lapsed grant is not handled here -- it is routed to the
// one AdminGate AdminShell owns (useCloseSurfaceOnReauth).
//
// Retries are off too, and already off globally: main.tsx sets retry: false
// on every query. Neither hook below sets its own, and neither should -- a
// retried 503 would be four audit rows per failed page load and several
// seconds of spinner before the unavailability copy ever appeared.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  adminMailListSchema,
  adminMailMessageSchema,
  type AdminMailList,
  type AdminMailMessage,
} from "./adminOutboxSchemas";

// The service's own clamps, mirrored so the page can say "showing the newest
// 50" without a round trip. They must match usecase/admin_outbox.go.
export const OUTBOX_DEFAULT_LIMIT = 50;
export const OUTBOX_MAX_LIMIT = 200;

export function adminMailPath(limit: number): string {
  return `/api/v1/admin/mail?limit=${String(limit)}`;
}

export function adminMailKey(limit: number) {
  return ["admin", "mail", { limit }] as const;
}

export function adminMailMessageKey(messageId: string) {
  return ["admin", "mail", "message", messageId] as const;
}

async function fetchAdminMail(limit: number): Promise<AdminMailList> {
  const body = await apiFetch<unknown>(adminMailPath(limit));
  return adminMailListSchema.parse(body);
}

async function fetchAdminMailMessage(
  messageId: string,
): Promise<AdminMailMessage> {
  const body = await apiFetch<unknown>(
    `/api/v1/admin/mail/${encodeURIComponent(messageId)}`,
  );
  return adminMailMessageSchema.parse(body);
}

export function useAdminMail(limit: number) {
  return useQuery({
    queryKey: adminMailKey(limit),
    queryFn: () => fetchAdminMail(limit),
    refetchOnWindowFocus: false,
  });
}

export function useAdminMailMessage(messageId: string) {
  return useQuery({
    queryKey: adminMailMessageKey(messageId),
    queryFn: () => fetchAdminMailMessage(messageId),
    refetchOnWindowFocus: false,
  });
}
```

- [ ] **Step 5: Run the test and watch it pass**

```bash
cd web && npx vitest run src/features/admin/adminOutboxSchemas.test.ts
```

Expected: PASS, three tests.

- [ ] **Step 6: Mutation-check the strictness**

Drop `.strict()` from `adminMailSummarySchema`. Re-run. Expected: **FAIL** on
"refuses a listed message that carries a body". Restore and re-run to green.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/admin/adminOutboxSchemas.ts web/src/features/admin/adminOutboxSchemas.test.ts web/src/features/admin/useAdminOutbox.ts
git commit -m "feat(web): schemas and queries for the outbound mail routes"
```

---

### Task 7: `AdminMailPage`

**Files:**
- Create: `web/src/features/admin/AdminMailPage.tsx`
- Test: `web/src/features/admin/AdminMailPage.test.tsx`
- Modify: `web/src/features/admin/AdminShell.tsx`, `web/src/routes/router.tsx`,
  `web/src/features/admin/adminBundleSplit.test.ts`

**Interfaces:**
- Consumes: everything Task 6 produced, plus `useCloseSurfaceOnReauth` from
  `./useAdminDirectory` and `isAdminLayerFailure` from `./useAdmin`.
- Produces: `AdminMailPage` (list, no props) and `AdminMailMessagePage`
  (`{ messageId: string }`), both named exports, both lazily loaded.

**Two routes, not a modal**, because opening a message is a distinct audited
request and a URL the operator can return to:

```
/admin/mail                the list
/admin/mail/$messageId     one message
```

- [ ] **Step 1: Write the failing test**

Create `web/src/features/admin/AdminMailPage.test.tsx`. Follow
`AdminHouseholdsPage.test.tsx` for how this codebase mounts a page with a
`QueryClientProvider` and a stubbed `fetch` — read that file first and reuse
its helper rather than inventing a second one.

```tsx
import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
// renderWithRouter / the fetch stub helper: import exactly what
// AdminHouseholdsPage.test.tsx imports.

describe("AdminMailPage", () => {
  it("lists what the outbox holds, with no body text on a row", async () => {
    // Stub GET /api/v1/admin/mail?limit=50 with:
    // { messages: [{ id: "0OQ1sV2mB7hN4kR8xT3wZq", to: "chris@example.com",
    //   subject: "Your Hearth sign-in link", sentAt: "2026-09-04T09:12:33Z" }],
    //   total: 1, truncated: false }
    // Render <AdminMailPage />.
    expect(await screen.findByText("chris@example.com")).toBeInTheDocument();
    expect(screen.getByText("Your Hearth sign-in link")).toBeInTheDocument();
  });

  it("says the inspector is not configured, and names the variable", async () => {
    // Stub the same path with 503
    // { error: { code: "MAIL_INSPECTOR_NOT_CONFIGURED", message: "…" } }
    expect(await screen.findByText(/MAILPIT_API_URL/)).toBeInTheDocument();
  });

  it("says Mailpit is not answering, which is a different message", async () => {
    // Stub the same path with 502
    // { error: { code: "MAIL_UPSTREAM_UNAVAILABLE", message: "…" } }
    expect(await screen.findByText(/not answering/i)).toBeInTheDocument();
    expect(screen.queryByText(/MAILPIT_API_URL/)).not.toBeInTheDocument();
  });

  it("says the store is not durable, so a missing message is not a bug", async () => {
    // The list stub from the first test.
    expect(
      await screen.findByText(/Mailpit keeps these only until it restarts/i),
    ).toBeInTheDocument();
  });

  // Decision 10's third row. A 404 on this route is Mailpit having dropped
  // the message, not the admin surface disappearing -- if this rendered
  // nothing, the operator would meet a blank screen for an ordinary event.
  it("says a dropped message is gone rather than rendering nothing", async () => {
    // Stub GET /api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq with 404
    // { error: { code: "NOT_FOUND", message: "That could not be found." } }
    // Render <AdminMailMessagePage messageId="0OQ1sV2mB7hN4kR8xT3wZq" />.
    expect(
      await screen.findByText(/no longer holds this message/i),
    ).toBeInTheDocument();
  });

  it("shows a message's links with a copy control, and never renders html", async () => {
    // Stub GET /api/v1/admin/mail/0OQ1sV2mB7hN4kR8xT3wZq with:
    // { id: "…", to: "chris@example.com", subject: "…",
    //   sentAt: "2026-09-04T09:12:33Z",
    //   links: ["https://oink.mywire.org/sign-in/magic?token=abc123"],
    //   text: "Open https://oink.mywire.org/sign-in/magic?token=abc123 to sign in." }
    // Render <AdminMailMessagePage messageId="0OQ1sV2mB7hN4kR8xT3wZq" />.
    expect(
      await screen.findByText(
        "https://oink.mywire.org/sign-in/magic?token=abc123",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /copy link/i }),
    ).toBeInTheDocument();
  });
});
```

Fill each comment in with the real stub before running — a comment is not a
test. The last assertion of the third case is the one that matters most: the
two unavailability states must not render the same copy.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/admin/AdminMailPage.test.tsx
```

Expected: FAIL, cannot resolve `./AdminMailPage`.

- [ ] **Step 3: Write the page**

Create `web/src/features/admin/AdminMailPage.tsx`. The list follows
`AdminHouseholdsPage.tsx` — `PageContainer`, `useCloseSurfaceOnReauth`, the
`isAdminLayerFailure` filter on the inline error, `Link` for navigation. The
**message view follows `AdminHouseholdPage.tsx`** instead, because that file
is the one that already separates its own 404 from the gate's; read it before
writing the second component.

```tsx
// The operator's outbound mail. Two components, two routes: the list, and one
// message. Opening a message is a separate request on purpose -- it is its
// own audit row, which is what makes seeing a live link a deliberate act with
// a record rather than the default state of a screen.
//
// Nothing here renders a message's HTML. The links are extracted server-side
// (domain.ExtractLinks) and arrive as strings; the body arrives as plain
// text. See the spec's decision 1 for what was rejected and why.
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import {
  OUTBOX_DEFAULT_LIMIT,
  useAdminMail,
  useAdminMailMessage,
} from "./useAdminOutbox";
import { isNotFound, useCloseSurfaceOnReauth } from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

// The two unavailable states get different copy because they have different
// fixes: one is an unset variable, the other is a container that is down.
// Anything else falls through to the generic message the error carries.
function outboxErrorCopy(error: unknown): string | null {
  if (!(error instanceof ApiError)) return null;
  if (error.code === "MAIL_INSPECTOR_NOT_CONFIGURED") {
    return "The message inspector is not configured on this install. Set MAILPIT_API_URL and restart the API.";
  }
  if (error.code === "MAIL_UPSTREAM_UNAVAILABLE") {
    return "Mailpit is not answering. The messages are not lost — the reader is.";
  }
  return error.message;
}

export function AdminMailPage() {
  const query = useAdminMail(OUTBOX_DEFAULT_LIMIT);
  useCloseSurfaceOnReauth(query.error);

  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
        Outbound mail
      </h1>
      {/* Decision 9: the production Mailpit service has no volume, so this
          is the honest boundary of the screen rather than a defect to
          report. */}
      <p className="mt-1 text-[13px] text-muted">
        Mailpit keeps these only until it restarts — a deploy or a reboot
        clears them. Send a fresh link rather than looking for an old one.
      </p>

      {inlineError && (
        <p role="alert" className="mt-4 text-[13px]">
          {outboxErrorCopy(inlineError)}
        </p>
      )}

      {/* One row per message: recipient, subject, time. No body text, ever
          -- see adminOutboxSchemas.ts. */}
      {/* ... the list, following AdminHouseholdsPage's row markup ... */}
    </PageContainer>
  );
}

// The message view follows AdminHouseholdPage.tsx, NOT the list page above,
// and the difference is the whole point: isAdminLayerFailure treats
// NOT_FOUND as "the admin surface is gone, let AdminGate handle it", which is
// right on a list and wrong here. On this route a 404 means Mailpit no longer
// holds the message -- ordinary, expected (its store has no volume), and the
// screen has its own copy for it. So the miss is checked FIRST, before the
// gate filter, exactly as the households drill-in does.
export function AdminMailMessagePage({ messageId }: { messageId: string }) {
  const query = useAdminMailMessage(messageId);
  useCloseSurfaceOnReauth(query.error);

  if (isNotFound(query.error)) {
    return (
      <PageContainer>
        <Link to="/admin/mail" className="text-[12.5px] font-medium text-muted hover:text-ink">
          ‹ Outbound mail
        </Link>
        <p className="mt-4 text-[13px]">
          Mailpit no longer holds this message. Its store is cleared whenever
          the container restarts — send a fresh link rather than looking for
          this one.
        </p>
      </PageContainer>
    );
  }

  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  // ... inlineError rendered as on AdminHouseholdPage, then the links list
  // with a copy control per link, then the plain text ...
}
```

Complete both components. The copy control writes to
`navigator.clipboard.writeText(link)`; guard the call, because
`navigator.clipboard` is undefined in a non-secure context and in jsdom
unless the test provides it.

- [ ] **Step 4: Add the nav link**

In `web/src/features/admin/AdminShell.tsx`, add to `OperatorNav`'s `items`,
**between Flags and Households** as the spec's §8 says:

```tsx
    { to: "/admin/flags", label: "Flags" },
    { to: "/admin/mail", label: "Mail" },
    { to: "/admin/households", label: "Households" },
```

- [ ] **Step 5: Add the routes**

In `web/src/routes/router.tsx`, beside the other lazy admin components:

```tsx
const LazyAdminMailPage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailPage,
  })),
);
const LazyAdminMailMessagePage = lazy(() =>
  import("../features/admin/AdminMailPage").then((m) => ({
    default: m.AdminMailMessagePage,
  })),
);
```

Two `createRoute` calls under `adminRoute`, `path: "mail"` and
`path: "mail/$messageId"`, each with the same inlined `Suspense` fallback the
neighbouring admin routes use, the second reading `messageId` with
`useParams({ from: "/authenticated/admin/mail/$messageId" })`. Add both to
`adminRoute.addChildren([...])`.

Update the route map comment at the top of the file (lines 23–26) so it lists
the two new routes.

- [ ] **Step 6: Extend the bundle-split test**

In `web/src/features/admin/adminBundleSplit.test.ts`, add to the first test:

```ts
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "AdminMailPage.tsx"),
    );
    expect(reachable).not.toContain(
      join(SRC_ROOT, "features", "admin", "useAdminOutbox.ts"),
    );
```

- [ ] **Step 7: Run the tests and watch them pass**

```bash
cd web && npx vitest run src/features/admin/
cd web && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 8: Mutation-check the copy split**

Make `outboxErrorCopy` return the same string for both codes. Re-run.
Expected: **FAIL** on "says Mailpit is not answering, which is a different
message". Restore and re-run to green.

- [ ] **Step 9: Commit**

```bash
cd .. && make lint
git add web/src
git commit -m "feat(web): the operator's outbound mail screen"
```

---

### Task 8: The documents

Part of the work, not a tidy-up after it. `CLAUDE.md` names the first three
as documents that must not go stale.

**Files:**
- Modify: `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`,
  `docs/LEARNING.md`, `docs/INFRASTRUCTURE.md`,
  `docs/ADMIN_SURFACE_HANDOVER.md`, `docs/HANDOVER.md`,
  `deploy/.env.example`, `deploy/README.md`

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md`**

Use the **`maintaining-system-design`** skill. This change adds a port, an
adapter, two routes and a screen — every one of its triggers. Update the
prose under the diagrams too; that is where the reasoning lives.

- [ ] **Step 2: `docs/FEATURE_TRACKER.md`**

Move section 9's "Outbound message inspector" row from ⬜ to ✅ — or to 🟡
**with the gap named**, if the browser walk in Task 9 leaves one. Name the
spec and the verification file in the row, the way the households row does.

Then **recount the summary table by counting symbols, not by adjusting the
numbers**:

```bash
awk '/^## 9/,/^## Suggested/' docs/FEATURE_TRACKER.md | grep -c '| ✅ |'
awk '/^## 9/,/^## Suggested/' docs/FEATURE_TRACKER.md | grep -c '| 🟡 |'
awk '/^## 9/,/^## Suggested/' docs/FEATURE_TRACKER.md | grep -c '| ⬜ |'
awk '/^## 9/,/^## Suggested/' docs/FEATURE_TRACKER.md | grep -c '| 🚫 |'
```

Re-sum the Total row from the nine section rows as they then stand. Update
"Where things stand" at the top.

Update "Suggested order": item 3 is done, so the operator surface's remaining
work is item 4 alone, the read-only database browse.

- [ ] **Step 3: `docs/LEARNING.md`**

Add the `link-check` trap, whether or not anyone tripped it. It belongs in
the log because the next person to read Mailpit's API will find that endpoint
and think it is a gift:

> **An endpoint that "checks" your links visits them.** Mailpit's
> `/api/v1/message/{id}/link-check` reports each URL's status by issuing a
> real HTTP request to it. Every URL in a Hearth email is a live single-use
> token on a public host, so calling it would be asking Mailpit to spend the
> links the screen exists to hand out. Caught while reading v1.30.5's source
> rather than by a test — nothing would have gone red. What holds the rule
> now is an adapter test asserting that only two upstream paths are ever
> requested.

If a defect surfaces during Task 9's walk, add it too. If it matches an
existing pattern (1 through 16), add it there as evidence rather than
starting a new section.

- [ ] **Step 4: `docs/INFRASTRUCTURE.md`**

Line 198 already names `MAILPIT_API_URL` as something an unbuilt feature will
need. Make that sentence true: name the feature as built, and say what
happens when the variable is unset.

- [ ] **Step 5: `deploy/.env.example`**

Add the variable with a comment saying what is unavailable without it:

```bash
# The operator's outbound message inspector (/admin/mail) reads Mailpit's
# HTTP API over the compose network. Unset means that one admin screen says
# it is not configured; nothing else changes. `mailpit` is a real service in
# deploy/docker-compose.prod.yml and `api` already depends on it, so this
# needs no compose change.
MAILPIT_API_URL=http://mailpit:8025
```

- [ ] **Step 6: `deploy/README.md`**

The tunnel section (around lines 149–177) currently describes the only way to
hand someone a link. It is no longer the only way — say so, and keep the
tunnel documented as the fallback for when the API itself is the thing that
is broken.

- [ ] **Step 7: `docs/ADMIN_SURFACE_HANDOVER.md` and `docs/HANDOVER.md`**

Move the inspector out of "What does NOT exist" (§3) into "What exists" (§2)
in the admin handover, with what the walk found. In `docs/HANDOVER.md` §4,
update the sentence that says two of the four remain — one does.

- [ ] **Step 8: Commit**

```bash
git add docs deploy
git commit -m "docs: record the outbound message inspector"
```

---

### Task 9: The browser walk

**A feature is not done because its tests pass.** The product owner asked for
this explicitly on 2026-07-30, and every ✅ in the tracker carries it. Two of
this feature's states — unconfigured and unreachable — are invisible to a
green suite and are exactly the states a real install meets.

**Files:**
- Create: `docs/superpowers/plans/2026-09-04-hearth-outbound-inspector-verification.md`

- [ ] **Step 1: Bring the stack up with the inspector configured**

```bash
make dev     # http://localhost:5173
```

Set `MAILPIT_API_URL=http://mailpit:8025` in the development environment the
API reads, and restart the API so it picks the variable up. Sign in as
`andreas@hearth.family` / `hearth-dev-password` (from `usecase/seed.go`);
that account already holds a `platform_admins` row.

Generate real mail first, so the list has something true in it: request a
magic link, and send an invite from Settings.

- [ ] **Step 2: Walk fifteen criteria, recording each one as it happens**

Write the file as you go, criterion by criterion, in the shape
`docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`
uses. Not afterwards from memory.

1. `/admin` re-auth prompt appears; the correct password opens the surface.
2. The operator nav shows Flags · Households · Mail, and Mail is visibly the
   active link when it is the page on screen.
3. `/admin/mail` lists the magic-link and invite messages, newest first.
4. A list row shows recipient, subject and time — **and no body text**.
   Confirm in the DOM, not only by eye.
5. The durability line is on the page.
6. Opening a message shows its links, and the plain-text body below them.
7. The link shown is the real one: paste it into a private window and it
   signs in or opens the invite. This is the criterion the whole feature
   exists for.
8. Copy link puts the URL on the clipboard, whole — paste it somewhere and
   compare the last character, which is where a bad strip set would show.
9. `/admin/mail/notarealid22chars0` answers a refusal, not a server error.
10. A second view of the same message writes exactly one more
    `admin_audit_log` row, and its `detail` contains no URL and no body:
    ```bash
    docker exec hearth-postgres-1 psql -U hearth -d hearth \
      -c "select action, target, detail from admin_audit_log order by at desc limit 3;"
    ```
11. Stop Mailpit (`docker compose stop mailpit`) and reload: the page says
    Mailpit is not answering, and does **not** name `MAILPIT_API_URL`.
    Start it again.
12. Restart the API with `MAILPIT_API_URL` unset: the page says it is not
    configured and names the variable. Restore the variable.
13. A non-admin household member gets a 404 on `/api/v1/admin/mail` — the
    surface is invisible to them, unchanged by this feature.
14. No horizontal overflow at 305, 360, 768 and 1440 px, on both screens.
15. The browser console carries no error on either screen.

- [ ] **Step 3: Fix what the walk finds, in the same branch**

A defect found here is fixed here, with a test that would have caught it, and
recorded in `docs/LEARNING.md`. If the fix is one instance of a class, sweep
the class — `hunting-sibling-defects` exists because fixing one instance has
failed to fix the class five times in this repository.

- [ ] **Step 4: Final full run**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: both green. Report the result honestly; if anything fails, say so
with the output.

- [ ] **Step 5: Commit and open the pull request**

```bash
git add docs/superpowers/plans/2026-09-04-hearth-outbound-inspector-verification.md
git commit -m "docs: the outbound message inspector's browser walk"
git push -u origin admin-outbox
gh pr create --title "Outbound message inspector: hand someone a link without an SSH tunnel" --body "…"
```

---

## Self-review

**Spec coverage** — every section of the spec has a task:

| Spec | Task |
|---|---|
| §3 decision 1 (links + text) | 1, 2, 7 |
| §3 decision 2 (never link-check) | 3, and its own test |
| §3 decision 3 (extraction in domain) | 1 |
| §3 decision 4 (text first, HTML fallback) | 1 |
| §3 decision 5 (its own service) | 2 |
| §3 decision 6 (port vs view type) | 2 |
| §3 decision 7 (no snippet in the list) | 3, 5, 6 |
| §3 decision 8 (no search) | absent by construction — no query parameter anywhere but `limit` |
| §3 decision 9 (ephemeral store) | 7 (the copy), 8 (the docs), 9 (criterion 5) |
| §3 decision 10 (503 vs 502) | 4, 5, 7, 9 (criteria 11–12) |
| §3 decision 11 (nil service, route registered) | 5 |
| §3 decision 12 (id validation) | 5, 9 (criterion 9) |
| §3 decision 13 (audit names the message) | no code — the existing middleware already does this; 9 (criterion 10) proves it |
| §3 decision 14 (no feature flag) | absent by construction |
| §3 decision 15 (reading marks read) | 3 (documented in the adapter) |
| §4 layers | the file table above |
| §5 `ExtractLinks` rules | 1, one test per rule |
| §6 routes and DTOs | 5 |
| §7 configuration | 4 |
| §8 frontend | 6, 7 |
| §9 testing | every task's own steps |
| §10 documentation | 8 |
| §11 rollout and the walk | 9 |

**Placeholders — one knowing deviation, named rather than hidden.** Task 7's
test file is stubs plus assertions rather than complete code, and the skill
calls that a plan failure. The deviation is deliberate: this codebase's page
tests are built on a render/fetch-stub helper that
`AdminHouseholdsPage.test.tsx` owns, and transcribing a guessed copy of it
here would put a second, subtly different harness into the repository — the
worse outcome of the two. Every case carries its exact stub payload and its
real assertion, so what is missing is the harness call, not the test's
meaning. Task 7's step 1 says to read that file first. Task 7's component
body is likewise partial for a reason stated in place: the list markup is a
copy of the households list's rows, and the parts that are *not* a copy — the
error copy split, the 404 branch, the durability line — are written out in
full.

**Type consistency:** `OutboxPage`, `OutboxMessage`, `OutboxListing`,
`OutboxMessageView` are defined in Task 2 and used with those names in Tasks
3, 5 and 6; the JSON keys in Task 5's DTOs (`id`, `to`, `subject`, `sentAt`,
`links`, `text`, `messages`, `total`, `truncated`) are the same keys Task 6's
Zod schemas parse and Task 5's tests assert exactly.
