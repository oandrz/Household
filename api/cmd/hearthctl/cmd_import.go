package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// transaction import: many rows from a CSV, one POST /transactions each,
// every row carrying an idempotency key so the same file run twice creates
// nothing new. No server route exists for bulk insert on purpose -- rows are
// independent and replay-safe, so a client-side loop is correct and the
// server stays simple (spec decision 8).

// importColumns is the header vocabulary. Any other header is an error
// rather than ignored: a misspelt column silently dropped would import a
// whole statement with, say, no categories.
var importColumns = map[string]bool{
	"date": true, "kind": true, "description": true, "amount_minor": true,
	"from_account": true, "to_account": true, "category": true, "paid_by": true,
	"key": true, "received_minor": true,
}

// importRow is one CSV line after parsing and name resolution, ready to
// become a request body. line is the 1-based CSV line for error messages.
type importRow struct {
	line int
	key  string
	body map[string]any
}

type importSummary struct {
	Created  int             `json:"created"`
	Replayed int             `json:"replayed"`
	Failed   []importFailure `json:"failed"`
}

type importFailure struct {
	Line  int    `json:"line"`
	Error string `json:"error"`
}

func cmdImport(ctx context.Context, c *client, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("transaction import", stderr)
	dryRun := fs.Bool("dry-run", false, "parse and resolve every row, print them, send nothing")
	positional, flagArgs := splitPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		return fail(exitUsage, "")
	}
	if len(positional) != 1 {
		return fail(exitUsage, "usage: hearthctl transaction import <file.csv> [--dry-run]")
	}
	f, err := os.Open(positional[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	defer f.Close()

	if err := c.requireCreds(); err != nil {
		return err
	}
	names, err := loadNames(ctx, c)
	if err != nil {
		return err
	}
	rows, problems, err := parseImport(f, names)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		// Nothing is sent when any row is malformed: a statement is one
		// unit of work to the person who exported it, and half of one
		// imported is harder to reason about than none.
		json.NewEncoder(stdout).Encode(importSummary{Failed: problems})
		return fail(exitUsage, "%d row(s) could not be resolved; nothing was sent", len(problems))
	}
	if *dryRun {
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			m := map[string]any{"line": r.line, "key": r.key}
			for k, v := range r.body {
				m[k] = v
			}
			out = append(out, m)
		}
		return json.NewEncoder(stdout).Encode(out)
	}

	var summary importSummary
	summary.Failed = []importFailure{}
	for _, r := range rows {
		raw, _ := json.Marshal(r.body)
		res, err := c.doWithHeaders(ctx, http.MethodPost, "/transactions", raw, map[string]string{idempotencyKeyHeader: r.key})
		if err != nil {
			// Network failure: stop here. Every row so far is keyed, so
			// re-running the file after the server is back is safe, and
			// continuing would only produce one identical error per row.
			json.NewEncoder(stdout).Encode(summary)
			return err
		}
		switch res.status {
		case http.StatusCreated:
			summary.Created++
		case http.StatusOK:
			summary.Replayed++
		default:
			summary.Failed = append(summary.Failed, importFailure{Line: r.line, Error: strings.TrimSpace(string(res.body))})
		}
	}
	json.NewEncoder(stdout).Encode(summary)
	if len(summary.Failed) > 0 {
		return fail(exitAPIRefused, "%d row(s) refused by the API; %d created, %d replayed", len(summary.Failed), summary.Created, summary.Replayed)
	}
	fmt.Fprintf(stderr, "%d created, %d replayed\n", summary.Created, summary.Replayed)
	return nil
}

// nameIndex resolves a name or an id to an id. Names are matched
// case-insensitively and must be unambiguous; an id is passed through only
// if it is one the household actually has, so a typo in an id fails here
// with a line number rather than at the server as a bare 422.
type nameIndex struct {
	label string
	byID  map[string]bool
	byLow map[string][]string
}

func newNameIndex(label string) *nameIndex {
	return &nameIndex{label: label, byID: map[string]bool{}, byLow: map[string][]string{}}
}

func (n *nameIndex) add(id, name string) {
	n.byID[id] = true
	low := strings.ToLower(strings.TrimSpace(name))
	n.byLow[low] = append(n.byLow[low], id)
}

func (n *nameIndex) resolve(value string) (string, error) {
	value = strings.TrimSpace(value)
	if n.byID[value] {
		return value, nil
	}
	ids := n.byLow[strings.ToLower(value)]
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return "", fmt.Errorf("unknown %s %q", n.label, value)
	default:
		return "", fmt.Errorf("%s %q matches %d entries; use the id", n.label, value, len(ids))
	}
}

type householdNames struct {
	accounts, categories, members *nameIndex
}

// loadNames fetches the three lists once. Archived accounts and
// categories are included so a statement that predates an archive still
// resolves; the server decides whether writing to one is allowed.
func loadNames(ctx context.Context, c *client) (householdNames, error) {
	names := householdNames{
		accounts:   newNameIndex("account"),
		categories: newNameIndex("category"),
		members:    newNameIndex("member"),
	}
	var accounts struct {
		Accounts []struct {
			ID, Nickname string
		}
	}
	if err := c.getJSON(ctx, "/accounts?include_archived=true", &accounts); err != nil {
		return names, err
	}
	for _, a := range accounts.Accounts {
		names.accounts.add(a.ID, a.Nickname)
	}
	var categories struct {
		Categories []struct{ ID, Name string }
	}
	if err := c.getJSON(ctx, "/categories?includeArchived=true", &categories); err != nil {
		return names, err
	}
	for _, cat := range categories.Categories {
		names.categories.add(cat.ID, cat.Name)
	}
	var members []struct {
		ID   string
		User struct{ DisplayName, Email string }
	}
	if err := c.getJSON(ctx, "/household/members", &members); err != nil {
		return names, err
	}
	for _, m := range members {
		names.members.add(m.ID, m.User.DisplayName)
		if m.User.Email != "" {
			names.members.add(m.ID, m.User.Email)
		}
	}
	return names, nil
}

func (c *client) getJSON(ctx context.Context, path string, into any) error {
	res, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := refuse(res, io.Discard); err != nil {
		// refuse already chose the exit code -- 2 for a lapsed session, 3
		// for anything else -- and the skill tells an agent to act on that
		// code. Rewrapping here as exit 3 would turn "sign in again" into
		// "a field is wrong". Only the message is improved.
		var ee *exitError
		if errors.As(err, &ee) {
			ee.msg = fmt.Sprintf("GET %s answered %d: %s", path, res.status, strings.TrimSpace(string(res.body)))
			return ee
		}
		return err
	}
	if err := json.Unmarshal(res.body, into); err != nil {
		return fail(exitAPIRefused, "GET %s: cannot read the response: %v", path, err)
	}
	return nil
}

// parseImport reads the whole file, resolves every row, and returns the
// rows in file order plus every row-level problem. A malformed file (no
// header, an unknown column) is a hard error; a bad row is a problem.
func parseImport(r io.Reader, names householdNames) ([]importRow, []importFailure, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fail(exitUsage, "the file has no header row")
	}
	col := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if !importColumns[h] {
			return nil, nil, fail(exitUsage, "unknown column %q; allowed: date, kind, description, amount_minor, from_account, to_account, category, paid_by, key, received_minor", h)
		}
		col[h] = i
	}
	for _, required := range []string{"date", "kind", "description", "amount_minor"} {
		if _, ok := col[required]; !ok {
			return nil, nil, fail(exitUsage, "the header is missing the %q column", required)
		}
	}
	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var rows []importRow
	var problems []importFailure
	seen := map[string]int{}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			problems = append(problems, importFailure{line, err.Error()})
			continue
		}
		body, fingerprint, err := buildImportBody(rec, get, names)
		if err != nil {
			problems = append(problems, importFailure{line, err.Error()})
			continue
		}
		key := get(rec, "key")
		if key == "" {
			key = deriveKey(fingerprint, seen)
		}
		rows = append(rows, importRow{line: line, key: key, body: body})
	}
	return rows, problems, nil
}

// deriveKey is spec decision 12: a hash of the row's content, plus "#n" for
// the n-th identical row in the same file. A hash alone would fold two
// genuine identical coffees into one; a line number alone would re-key
// every row below an edit and duplicate them on the next run.
func deriveKey(fingerprint string, seen map[string]int) string {
	sum := sha256.Sum256([]byte(fingerprint))
	key := hex.EncodeToString(sum[:])[:32]
	seen[key]++
	if n := seen[key]; n > 1 {
		return fmt.Sprintf("%s#%d", key, n)
	}
	return key
}

// buildImportBody turns one record into the exact POST /transactions body
// and the canonical string the default key is hashed from. The fingerprint
// uses resolved ids, not the names typed, so renaming an account later does
// not re-key an already-imported row.
func buildImportBody(rec []string, get func([]string, string) string, names householdNames) (map[string]any, string, error) {
	kind := get(rec, "kind")
	date := get(rec, "date")
	desc := get(rec, "description")
	if date == "" || kind == "" || desc == "" {
		return nil, "", fmt.Errorf("date, kind and description are required")
	}
	amount, err := strconv.ParseInt(get(rec, "amount_minor"), 10, 64)
	if err != nil || amount <= 0 {
		return nil, "", fmt.Errorf("amount_minor must be a whole number of minor units greater than 0")
	}
	body := map[string]any{"kind": kind, "occurredOn": date, "description": desc, "amountMinor": amount}

	var from, to, category, paidBy string
	if v := get(rec, "from_account"); v != "" {
		if from, err = names.accounts.resolve(v); err != nil {
			return nil, "", err
		}
	}
	if v := get(rec, "to_account"); v != "" {
		if to, err = names.accounts.resolve(v); err != nil {
			return nil, "", err
		}
	}
	if v := get(rec, "category"); v != "" {
		if category, err = names.categories.resolve(v); err != nil {
			return nil, "", err
		}
	}
	if v := get(rec, "paid_by"); v != "" {
		if paidBy, err = names.members.resolve(v); err != nil {
			return nil, "", err
		}
	}
	switch kind {
	case "expense":
		if from == "" {
			return nil, "", fmt.Errorf("an expense needs from_account")
		}
	case "income":
		if to == "" {
			return nil, "", fmt.Errorf("an income needs to_account")
		}
	case "transfer":
		if from == "" || to == "" {
			return nil, "", fmt.Errorf("a transfer needs from_account and to_account")
		}
	default:
		return nil, "", fmt.Errorf("kind must be expense, income or transfer, got %q", kind)
	}
	setIf(body, "fromAccountId", from)
	setIf(body, "toAccountId", to)
	setIf(body, "categoryId", category)
	setIf(body, "paidByMembershipId", paidBy)
	received := ""
	if v := get(rec, "received_minor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, "", fmt.Errorf("received_minor must be a whole number of minor units greater than 0")
		}
		body["receivedAmountMinor"] = n
		received = v
	}
	fingerprint := strings.Join([]string{date, kind, desc, strconv.FormatInt(amount, 10), from, to, category, paidBy, received}, "|")
	return body, fingerprint, nil
}
