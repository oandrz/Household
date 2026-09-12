package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// route is one line of the manual `routes` prints. guard says who may call
// it in the server's own vocabulary: session (any signed-in member), owner,
// money / marriage (the capability), csrf (a write; the CLI adds the header
// itself). body is the request shape, or "-" for none.
type route struct {
	method, path, guard, body string
}

// routeJSON is the shape `routes --json` prints: the same four fields with
// stable names, so an agent parses the table rather than the tabwriter
// layout.
type routeJSON struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Guard  string `json:"guard"`
	Body   string `json:"body"`
}

// routeTable is hand-maintained and checked by routes_test.go against
// internal/adapter/http/router.go: every path here must exist there, so a
// route that is renamed or removed fails the build rather than misleading
// the agent that reads this. Add a line when a route is added.
var routeTable = []route{
	{"POST", "/auth/sign-in", "public", `{"email","password"}`},
	{"POST", "/auth/sign-out", "session+csrf", "-"},
	{"GET", "/auth/me", "session", "-"},
	{"GET", "/auth/tokens", "session", "-"},
	{"POST", "/auth/tokens", "browser session+csrf (a token cannot mint a token)", `{"name","expiresInDays"?}` + " -> the raw token, shown once"},
	{"DELETE", "/auth/tokens/{id}", "browser session+csrf", "-"},
	{"GET", "/currencies", "public", "-"},
	{"GET", "/household", "session", "-"},
	{"PATCH", "/household", "owner+csrf", `{"name"?,"familyName"?,"primaryCurrency"?,"showSecondaryCurrency"?,"secondaryCurrency"?,"fxRateMode"?}`},
	{"GET", "/household/members", "session", "-"},
	{"POST", "/household/members/invite", "owner+csrf", `{"name","email","role","capabilities":[...]}`},
	{"PATCH", "/household/members/{id}", "owner+csrf", `{"role"?,"capabilities"?}`},
	{"DELETE", "/household/members/{id}", "owner+csrf", "-"},
	{"GET", "/spaces", "session", "-"},
	{"POST", "/spaces", "owner+csrf", `{"name","visibility","template"?}`},
	{"GET", "/notification-preferences", "session", "-"},
	{"PATCH", "/notification-preferences", "owner+csrf", `{"billReminders"?,"overspendAlerts"?,"retroReminder"?,"weeklyDigest"?}`},

	{"GET", "/auth/telegram", "browser session", "-"},
	{"DELETE", "/auth/telegram", "browser session+csrf", "-"},
	{"POST", "/auth/telegram/link", "browser session+csrf", "-"},
	{"GET", "/auth/telegram/link/{id}", "browser session", "-"},
	{"POST", "/auth/telegram/link/{id}/confirm", "browser session+csrf", "-"},

	{"GET", "/accounts", "money", "?include_archived=true"},
	{"POST", "/accounts", "money+owner+csrf", `{"nickname","type","ownerMembershipId"?,"openingBalanceMinor","openingBalanceCurrency","openingBalanceAsOf","countTowardNetWorth"?,"visibleToLimitedMembers"?}`},
	{"PATCH", "/accounts/{id}", "money+owner+csrf", `same fields, all optional`},
	{"POST", "/accounts/{id}/archive", "money+owner+csrf", "-"},
	{"POST", "/accounts/{id}/restore", "money+owner+csrf", "-"},

	{"GET", "/transactions", "money+owner", `?month=YYYY-MM&kind=&account_id=&category_id=&paid_by=&cursor=&limit=`},
	{"POST", "/transactions", "money+owner+csrf", `{"kind","occurredOn","description","amountMinor","fromAccountId"?,"toAccountId"?,"categoryId"?,"paidByMembershipId"?,"receivedAmountMinor"?} + header Idempotency-Key? (200 on replay, 409 on reuse)`},
	{"PATCH", "/transactions/{id}", "money+owner+csrf", `same fields, all optional`},
	{"DELETE", "/transactions/{id}", "money+owner+csrf", "-"},
	{"GET", "/categories", "money+owner", "?includeArchived=true"},
	{"POST", "/categories", "money+owner+csrf", `{"name"}`},
	{"PATCH", "/categories/{id}", "money+owner+csrf", `{"name"}`},
	{"POST", "/categories/{id}/archive", "money+owner+csrf", "-"},
	{"POST", "/categories/{id}/restore", "money+owner+csrf", "-"},
	{"GET", "/budgets/{month}", "money+owner", "month is YYYY-MM"},
	{"PUT", "/budgets/{month}", "money+owner+csrf", `{"expectedIncomeMinor"?,"lines":[{"categoryId","capMinor"}]}`},
	{"POST", "/budgets/{month}/rollover", "money+owner+csrf", `{"goalId"}`},
	{"GET", "/budgets/history", "money+owner", "?months=6"},
	{"GET", "/goals", "money+owner", "?include_archived=true"},
	{"POST", "/goals", "money+owner+csrf", `{"name","targetMinor","currency","targetMonth"?,"plannedMonthlyMinor","startingBalanceMinor"}`},
	{"PATCH", "/goals/{id}", "money+owner+csrf", `same fields, all optional`},
	{"POST", "/goals/{id}/archive", "money+owner+csrf", "-"},
	{"POST", "/goals/{id}/restore", "money+owner+csrf", "-"},
	{"GET", "/goals/{id}/contributions", "money+owner", "-"},
	{"POST", "/goals/{id}/contributions", "money+owner+csrf", `{"amountMinor","occurredOn","note"?,"currency"?}`},
	{"DELETE", "/goals/{id}/contributions/{contributionId}", "money+owner+csrf", "-"},

	// Holdings: the portfolio. Quantities cross as strings ("300.5"), never as
	// nano integers to be divided in a browser; amounts are minor units like
	// every other money field. A valuation POSTs but upserts -- one price per
	// holding per day -- and answers 200 rather than 201 for that reason.
	{"GET", "/holdings", "money+owner", "-"},
	{"GET", "/holdings/report", "money+owner", `?kind=quarter|half|year&count=N (default 6|4|3, max 12)`},
	{"POST", "/holdings", "money+owner+csrf", `{"accountId","name","instrument","unit","currency"?}`},
	{"PATCH", "/holdings/{id}", "money+owner+csrf", `{"name","instrument","unit"}`},
	{"POST", "/holdings/{id}/archive", "money+owner+csrf", "-"},
	{"POST", "/holdings/{id}/restore", "money+owner+csrf", "-"},
	{"GET", "/holdings/{id}/events", "money+owner", "-"},
	{"POST", "/holdings/{id}/events", "money+owner+csrf", `{"kind","quantity","amountMinor","primaryAmountMinor"?,"occurredOn","note"?}`},
	{"DELETE", "/holdings/{id}/events/{eventId}", "money+owner+csrf", "-"},
	{"GET", "/holdings/{id}/valuations", "money+owner", "-"},
	{"POST", "/holdings/{id}/valuations", "money+owner+csrf", `{"unitPriceMinor","primaryUnitPriceMinor"?,"asOf","note"?} -> 200, upsert`},
	{"GET", "/holdings/{id}/income", "money+owner", "-"},
	{"POST", "/holdings/{id}/income", "money+owner+csrf", `{"kind":"income"|"fee","amountMinor","primaryAmountMinor"?,"receivedOn","note"?}`},
	{"DELETE", "/holdings/{id}/income/{incomeId}", "money+owner+csrf", "-"},
	{"GET", "/bills", "money+owner", "?include_archived=true"},
	{"POST", "/bills", "money+owner+csrf", `{"name","amountMinor","cadence","nextDue","categoryId","payFromAccountId","paidByMembershipId","autopay","isSubscription"}`},
	{"PATCH", "/bills/{id}", "money+owner+csrf", `same fields, all optional`},
	{"POST", "/bills/{id}/archive", "money+owner+csrf", "-"},
	{"POST", "/bills/{id}/restore", "money+owner+csrf", "-"},
	{"POST", "/bills/{id}/pay", "money+owner+csrf", `{"paidOn","amountMinor"?}`},
	{"DELETE", "/bills/{id}/payments/{paymentId}", "money+owner+csrf", "-"},

	{"GET", "/retros", "marriage+owner", "-"},
	{"POST", "/retros", "marriage+owner+csrf", "- (the server picks the month)"},
	{"GET", "/retros/{month}", "marriage+owner", "-"},
	{"PATCH", "/retros/{month}", "marriage+owner+csrf", `{"mood"?,"wentWell","wasHard","notes","version"}`},
	{"POST", "/retros/{month}/complete", "marriage+owner+csrf", "-"},
	{"DELETE", "/retros/{month}", "marriage+owner+csrf", "-"},
	{"POST", "/retros/{month}/actions", "marriage+owner+csrf", `{"body","assigneeMembershipIds":[...],"carriedFrom"?}`},
	{"PATCH", "/retros/{month}/actions/{id}", "marriage+owner+csrf", `{"done"}`},
	{"DELETE", "/retros/{month}/actions/{id}", "marriage+owner+csrf", "-"},
	{"GET", "/marriage/vision", "marriage+owner", "-"},
	{"PUT", "/marriage/vision/{year}", "marriage+owner+csrf", `{"version","theme","description","pillars":[{"name","description","measures":[{"label","kind","current","target","goalId"?}]}],"milestones":[{"year","title","note"}]}`},
	{"GET", "/marriage/agreements", "marriage+owner", "-"},
	{"POST", "/marriage/agreements/sections", "marriage+owner+csrf", `{"name"}`},
	{"POST", "/marriage/agreements/starter-set", "marriage+owner+csrf", "-"},
	{"POST", "/marriage/agreements/proposals", "marriage+owner+csrf", `{"kind":"add|edit|remove","sectionId"(add),"targetAgreementId"(edit/remove),"body"(add/edit),"previousBody"(edit/remove),"note"?}`},
	{"POST", "/marriage/agreements/proposals/{id}/agree", "marriage+owner+csrf", "-"},
	{"POST", "/marriage/agreements/proposals/{id}/park", "marriage+owner+csrf", `{"note"?}`},
	{"POST", "/marriage/agreements/proposals/{id}/withdraw", "marriage+owner+csrf", "-"},
}

// cmdRoutes prints the table. It is a manual for whoever drives the CLI,
// human or agent, and the one place the whole API can be read at a glance
// without opening router.go. Body shapes marked `?` are optional; `{...}`
// means "see the handler" -- the shape is bigger than fits on a line, and
// `hearthctl api` passes any JSON through untouched.
func cmdRoutes(args []string, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "--json" {
		out := make([]routeJSON, 0, len(routeTable))
		for _, r := range routeTable {
			out = append(out, routeJSON{r.method, r.path, r.guard, r.body})
		}
		enc := json.NewEncoder(stdout)
		return enc.Encode(out)
	}
	if len(args) != 0 {
		return fail(exitUsage, "usage: hearthctl routes [--json]")
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "METHOD\tPATH\tWHO MAY CALL\tBODY")
	for _, r := range routeTable {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.method, r.path, r.guard, r.body)
	}
	fmt.Fprintln(tw, "")
	fmt.Fprintln(tw, "Paths are relative to /api/v1. 'csrf' means a write: hearthctl adds the header.")
	fmt.Fprintln(tw, "'money'/'marriage' is a capability; 'owner' is the role. Amounts are minor units.")
	return tw.Flush()
}
