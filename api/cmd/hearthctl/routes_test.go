package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The route table is prose an agent trusts, so it is checked against the
// code it describes: every method+path here must be registered in
// router.go and vice versa, or the build fails.
func TestRouteTableMatchesRouterGo(t *testing.T) {
	src, err := os.ReadFile("../../internal/adapter/http/router.go")
	if err != nil {
		t.Fatal(err)
	}
	// e.g.  w.Post("/accounts/{id}/archive", ...)
	re := regexp.MustCompile(`\.(Get|Post|Put|Patch|Delete)\("([^"]+)"`)
	registered := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		registered[strings.ToUpper(m[1])+" "+m[2]] = true
	}

	listed := map[string]bool{}
	for _, r := range routeTable {
		key := r.method + " " + r.path
		listed[key] = true
		if !registered[key] && !registeredUnderAuth(registered, r) {
			t.Errorf("routes lists %s but router.go does not register it", key)
		}
	}

	// Routes the CLI deliberately excludes: sign-up, magic link, Telegram
	// and invites are browser flows; the platform-admin surface (under
	// api.Route("/admin"): /session, /flags, /households, /db, /mail) sits
	// behind its own re-authentication; the family calendar is a dark stub;
	// health probes are not API.
	skip := regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) (/(sign-up|magic-link|telegram)|/invites|/session|/flags|/households|/db|/mail|/healthz|/readyz|/family)`)
	for key := range registered {
		if skip.MatchString(key) {
			continue
		}
		if !listed[key] && !listed[withAuthPrefix(key)] {
			t.Errorf("router.go registers %s but routes does not list it", key)
		}
	}
}

// router.go registers the auth routes inside api.Route("/auth", ...), so
// their literal strings lack the /auth prefix the table (correctly) shows.
func registeredUnderAuth(registered map[string]bool, r route) bool {
	return strings.HasPrefix(r.path, "/auth/") && registered[r.method+" "+strings.TrimPrefix(r.path, "/auth")]
}

func withAuthPrefix(key string) string {
	parts := strings.SplitN(key, " ", 2)
	return parts[0] + " /auth" + parts[1]
}

func TestRoutesJSONIsTheWholeTable(t *testing.T) {
	var buf strings.Builder
	if err := cmdRoutes([]string{"--json"}, &buf); err != nil {
		t.Fatal(err)
	}
	var got []routeJSON
	if err := json.Unmarshal([]byte(buf.String()), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if len(got) != len(routeTable) {
		t.Fatalf("printed %d routes, table has %d", len(got), len(routeTable))
	}
	if got[0].Method != routeTable[0].method || got[0].Path != routeTable[0].path {
		t.Fatalf("first row %+v does not match the table", got[0])
	}
}
