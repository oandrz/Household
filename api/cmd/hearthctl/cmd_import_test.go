package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
)

func testNames() householdNames {
	n := householdNames{
		accounts:   newNameIndex("account"),
		categories: newNameIndex("category"),
		members:    newNameIndex("member"),
	}
	n.accounts.add("acc-dbs", "DBS Savings")
	n.accounts.add("acc-ocbc", "OCBC")
	n.categories.add("cat-groc", "Groceries")
	n.categories.add("cat-dup-1", "Fun")
	n.categories.add("cat-dup-2", "fun")
	n.members.add("mem-a", "Andreas")
	return n
}

const importHeader = "date,kind,description,amount_minor,from_account,to_account,category,paid_by,key,received_minor\n"

func TestImportResolvesNamesCaseInsensitivelyAndKeepsIds(t *testing.T) {
	rows, problems, err := parseImport(strings.NewReader(importHeader+
		"2026-09-08,expense,Coffee,650,dbs savings,,GROCERIES,andreas,,\n"+
		"2026-09-08,income,Salary,650000,,acc-ocbc,,,,\n"), testNames())
	if err != nil || len(problems) != 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	if rows[0].body["fromAccountId"] != "acc-dbs" || rows[0].body["categoryId"] != "cat-groc" || rows[0].body["paidByMembershipId"] != "mem-a" {
		t.Fatalf("row 1 resolved to %v", rows[0].body)
	}
	if rows[1].body["toAccountId"] != "acc-ocbc" {
		t.Fatalf("row 2 resolved to %v", rows[1].body)
	}
	if _, has := rows[1].body["fromAccountId"]; has {
		t.Fatalf("an empty optional must be absent, not \"\"")
	}
}

func TestImportRefusesUnknownAmbiguousAndMalformedRows(t *testing.T) {
	_, problems, err := parseImport(strings.NewReader(importHeader+
		"2026-09-08,expense,Coffee,650,Nope Bank,,,,,\n"+ // unknown account
		"2026-09-08,expense,Coffee,650,DBS Savings,,Fun,,,\n"+ // ambiguous category
		"2026-09-08,expense,Coffee,0,DBS Savings,,,,,\n"+ // zero amount
		"2026-09-08,refund,Coffee,650,DBS Savings,,,,,\n"+ // bad kind
		"2026-09-08,income,Salary,100,,,,,,\n"), // income with no destination
		testNames())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 5 {
		t.Fatalf("want 5 problems, got %d: %v", len(problems), problems)
	}
	for i, want := range []string{"unknown account", "matches 2 entries", "amount_minor", "kind must be", "needs to_account"} {
		if problems[i].Line != i+2 || !strings.Contains(problems[i].Error, want) {
			t.Errorf("problem %d = %+v, want line %d containing %q", i, problems[i], i+2, want)
		}
	}
}

func TestImportRefusesAnUnknownColumn(t *testing.T) {
	_, _, err := parseImport(strings.NewReader("date,kind,description,amount_minor,acount\n"), testNames())
	if exitCode(t, err) != exitUsage || !strings.Contains(err.Error(), `"acount"`) {
		t.Fatalf("want a usage error naming the column, got %v", err)
	}
}

func TestImportKeysAreStableAndIdenticalRowsStayDistinct(t *testing.T) {
	file := importHeader +
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,,,\n" +
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,,,\n" + // the second coffee
		"2026-09-08,expense,Lunch,1200,DBS Savings,,Groceries,,my-own-key,\n"
	first, _, _ := parseImport(strings.NewReader(file), testNames())
	// Insert a row above: the existing rows' keys must not move.
	again, _, _ := parseImport(strings.NewReader(importHeader+
		"2026-09-01,income,Salary,100,,OCBC,,,,\n"+file[len(importHeader):]), testNames())

	if first[0].key == first[1].key {
		t.Fatalf("two identical rows must get distinct keys, both %q", first[0].key)
	}
	if !strings.HasSuffix(first[1].key, "#2") || !strings.HasPrefix(first[1].key, first[0].key) {
		t.Fatalf("the second identical row should be <hash>#2, got %q vs %q", first[1].key, first[0].key)
	}
	if first[2].key != "my-own-key" {
		t.Fatalf("an explicit key column must win, got %q", first[2].key)
	}
	if again[1].key != first[0].key || again[2].key != first[1].key {
		t.Fatalf("keys moved when a row was inserted above: %q/%q vs %q/%q", again[1].key, again[2].key, first[0].key, first[1].key)
	}
	if len(first[0].key) != 32 {
		t.Fatalf("derived key length %d, want 32 hex chars", len(first[0].key))
	}
}

// importServer is a fake API that serves the three lookup lists and counts
// transaction creates, answering 201 for a new key and 200 for a repeat.
func importServer(t *testing.T) (*fakeAPI, string, map[string]int) {
	f, srv := newFakeAPI(t)
	keys := map[string]int{}
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/accounts":
			w.Write([]byte(`{"accounts":[{"id":"acc-dbs","nickname":"DBS Savings"}]}`))
		case r.URL.Path == "/api/v1/categories":
			w.Write([]byte(`{"categories":[{"id":"cat-groc","name":"Groceries"}]}`))
		case r.URL.Path == "/api/v1/household/members":
			w.Write([]byte(`[{"id":"mem-a","user":{"displayName":"Andreas","email":"a@example.com"}}]`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/transactions":
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				t.Errorf("import must send an Idempotency-Key on every create")
			}
			// newFakeAPI already drained r.Body into f.bodies.
			var body map[string]any
			json.Unmarshal(f.bodies[len(f.bodies)-1], &body)
			if body["description"] == "Refused" {
				http.Error(w, `{"error":{"code":"CATEGORY_KIND_MISMATCH"}}`, http.StatusUnprocessableEntity)
				return
			}
			keys[key]++
			if keys[key] > 1 {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(201)
			}
			w.Write([]byte(`{"id":"t-` + key + `"}`))
		default:
			http.Error(w, `{"error":{"code":"NOT_FOUND"}}`, 404)
		}
	}
	return f, srv.URL, keys
}

func writeCSV(t *testing.T, content string) string {
	p := filepath.Join(t.TempDir(), "rows.csv")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportRunTwiceCreatesThenReplaysAndReportsRefusedRows(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	recordSleeps(t)
	_, url, keys := importServer(t)
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,Andreas,,\n"+
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,Andreas,,\n"+
		"2026-09-08,expense,Refused,100,DBS Savings,,Groceries,,,\n")

	out, _, err := run_(t, url, "", "transaction", "import", file)
	if exitCode(t, err) != exitAPIRefused {
		t.Fatalf("one refused row must exit 3, got %v", err)
	}
	var s importSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("summary not JSON: %q", out)
	}
	if s.Created != 2 || s.Replayed != 0 || len(s.Failed) != 1 || s.Failed[0].Line != 4 {
		t.Fatalf("first run summary %+v", s)
	}
	if len(keys) != 2 {
		t.Fatalf("two identical coffees must be two keys, got %v", keys)
	}

	// The refused row is still refused on the second run, so the exit code must
	// stay 3. Unchecked, a second run that crashed before printing would leave
	// s holding the first run's summary and fail for the wrong reason.
	out, _, err = run_(t, url, "", "transaction", "import", file)
	if exitCode(t, err) != exitAPIRefused {
		t.Fatalf("second run still has one refused row and must exit 3, got %v", err)
	}
	s = importSummary{}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("second run summary not JSON: %q", out)
	}
	if s.Created != 0 || s.Replayed != 2 {
		t.Fatalf("second run must replay both good rows and create nothing: %+v", s)
	}
	for k, n := range keys {
		if n != 2 {
			t.Fatalf("key %s sent %d times, want 2", k, n)
		}
	}
}

func TestImportDryRunResolvesButSendsNothing(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, url, keys := importServer(t)
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,,,\n")

	out, _, err := run_(t, url, "", "transaction", "import", file, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("dry run posted %v", keys)
	}
	for _, r := range f.seen {
		if r.Method != "GET" {
			t.Fatalf("dry run made a %s", r.Method)
		}
	}
	if !strings.Contains(out, `"fromAccountId":"acc-dbs"`) || !strings.Contains(out, `"key":"`) {
		t.Fatalf("dry run should print resolved rows with keys, got %s", out)
	}
}

func TestImportSendsNothingWhenAnyRowIsUnresolvable(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	_, url, keys := importServer(t)
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,,,\n"+
		"2026-09-08,expense,Coffee,650,Nope,,Groceries,,,\n")
	_, _, err := run_(t, url, "", "transaction", "import", file)
	if exitCode(t, err) != exitUsage || len(keys) != 0 {
		t.Fatalf("want exit 1 and no posts, got err=%v posts=%v", err, keys)
	}
}

func TestImportOnALapsedSessionExits2AndPostsNothing(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"UNAUTHENTICATED"}}`, http.StatusUnauthorized)
	}
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "stale", CSRF: "c"})
	file := writeCSV(t, importHeader+"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,,,\n")

	_, _, err := run_(t, srv.URL, "", "transaction", "import", file)
	if code := exitCode(t, err); code != exitSignInAgain {
		t.Fatalf("a 401 while loading names must be exit %d (sign in again), got %d: %v", exitSignInAgain, code, err)
	}
	for _, r := range f.seen {
		if r.Method == "POST" {
			t.Fatalf("nothing may be posted on a lapsed session")
		}
	}
}

func TestTransactionAddKeyBecomesTheHeaderNotTheBody(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	_, _, err := run_(t, srv.URL, "", "transaction", "add", "--kind=expense", "--date=2026-09-08",
		"--description=Coffee", "--amount-minor=650", "--from-account=a1", "--key=row-9")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.seen[0].Header.Get("Idempotency-Key"); got != "row-9" {
		t.Fatalf("header = %q", got)
	}
	if strings.Contains(string(f.bodies[0]), "row-9") || strings.Contains(string(f.bodies[0]), keyFlag) {
		t.Fatalf("the key leaked into the body: %s", f.bodies[0])
	}
}

// recordSleeps replaces the pause between requests for one test.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	old := sleep
	sleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sleep = old })
	return &slept
}

// limitedImportServer is importServer, except a row whose description is
// in refuseTimes answers 429 that many times before being accepted. sends
// counts every POST per description.
func limitedImportServer(t *testing.T, refuseTimes map[string]int, retryAfter string) (url string, sends map[string]int) {
	f, url, _ := importServer(t)
	inner := f.respond
	sends = map[string]int{}
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/transactions" {
			var body map[string]any
			json.Unmarshal(f.bodies[len(f.bodies)-1], &body)
			desc, _ := body["description"].(string)
			sends[desc]++
			if sends[desc] <= refuseTimes[desc] {
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				http.Error(w, `{"error":{"code":"RATE_LIMITED"}}`, http.StatusTooManyRequests)
				return
			}
		}
		inner(w, r)
	}
	return url, sends
}

func TestImportWaitsOutA429AndResendsTheSameRow(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	slept := recordSleeps(t)
	url, sends := limitedImportServer(t, map[string]int{"Coffee": 1}, "2")
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+
		"2026-09-08,expense,Coffee,650,DBS Savings,,Groceries,Andreas,,\n"+
		"2026-09-09,expense,Tea,300,DBS Savings,,Groceries,Andreas,,\n")

	out, _, err := run_(t, url, "", "transaction", "import", file)
	if err != nil {
		t.Fatalf("import: %v (%s)", err, out)
	}
	var s importSummary
	json.Unmarshal([]byte(out), &s)
	if s.Created != 2 || len(s.Failed) != 0 {
		t.Fatalf("summary %+v, want 2 created and nothing failed", s)
	}
	if sends["Coffee"] != 2 {
		t.Fatalf("Coffee sent %d times, want 2 (refused once, then resent)", sends["Coffee"])
	}
	if !slices.Contains(*slept, 2*time.Second) {
		t.Fatalf("slept %v, want a 2s wait from Retry-After", *slept)
	}
}

func TestImportGivesUpOnARowAfterTenRetriesAndCarriesOn(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	recordSleeps(t)
	url, sends := limitedImportServer(t, map[string]int{"Stuck": 1000}, "1")
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+
		"2026-09-08,expense,Stuck,650,DBS Savings,,Groceries,Andreas,,\n"+
		"2026-09-09,expense,Tea,300,DBS Savings,,Groceries,Andreas,,\n")

	out, _, err := run_(t, url, "", "transaction", "import", file)
	if exitCode(t, err) != exitAPIRefused {
		t.Fatalf("a row refused for good must exit 3, got %v", err)
	}
	var s importSummary
	json.Unmarshal([]byte(out), &s)
	if s.Created != 1 || len(s.Failed) != 1 || s.Failed[0].Line != 2 {
		t.Fatalf("summary %+v, want Tea created and line 2 failed", s)
	}
	if sends["Stuck"] != 11 {
		t.Fatalf("Stuck sent %d times, want 11 (one try and ten retries)", sends["Stuck"])
	}
}

func TestImportPacesItsRows(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	slept := recordSleeps(t)
	_, url, _ := importServer(t)
	st, _ := newStore(url)
	st.save(&credentials{BaseURL: url, Session: "s", CSRF: "c"})
	file := writeCSV(t, importHeader+
		"2026-09-08,expense,A,1,DBS Savings,,Groceries,,,\n"+
		"2026-09-08,expense,B,1,DBS Savings,,Groceries,,,\n"+
		"2026-09-08,expense,C,1,DBS Savings,,Groceries,,,\n")
	if _, _, err := run_(t, url, "", "transaction", "import", file); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{200 * time.Millisecond, 200 * time.Millisecond}
	if !slices.Equal(*slept, want) {
		t.Fatalf("slept %v, want %v: a pause before every row but the first", *slept, want)
	}
}

// TestImportPacingLeavesHalfTheHouseholdAllowance: the people using the app
// in the same household keep at least half the allowance while a file goes in.
func TestImportPacingLeavesHalfTheHouseholdAllowance(t *testing.T) {
	if float64(importRowsPerSecond)*2 > httpadapter.ProductionHouseholdLimits.PerSecond {
		t.Fatalf("importRowsPerSecond = %d uses more than half of the household's %v per second",
			importRowsPerSecond, httpadapter.ProductionHouseholdLimits.PerSecond)
	}
}

func TestRetryAfterIsReadSafely(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"": time.Second, "abc": time.Second, "0": time.Second, "-3": time.Second,
		"2": 2 * time.Second, "999": maxRetryAfter,
	} {
		h := http.Header{}
		if header != "" {
			h.Set("Retry-After", header)
		}
		if got := retryAfter(h); got != want {
			t.Errorf("Retry-After %q gives %v, want %v", header, got, want)
		}
	}
}
