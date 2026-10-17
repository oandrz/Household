package main

import (
	"net/http"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	httpadapter "github.com/andreasoentoro/hearth/api/internal/adapter/http"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
)

// nginxConf is the file the web image ships, read from the repository so a
// change to either side of a pairing fails here.
const nginxConf = "../../../web/nginx.conf"

func readNginxConf(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("read web/nginx.conf: %v", err)
	}
	return string(b)
}

// nginxSeconds reads a directive given in whole seconds, which must appear
// exactly wantCount times with one value.
func nginxSeconds(t *testing.T, conf, directive string, wantCount int) time.Duration {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + directive + `\s+(\d+)s;`)
	matches := re.FindAllStringSubmatch(conf, -1)
	if len(matches) != wantCount {
		t.Fatalf("%s appears %d times in web/nginx.conf, want %d", directive, len(matches), wantCount)
	}
	for _, m := range matches[1:] {
		if m[1] != matches[0][1] {
			t.Fatalf("%s differs between locations: %s and %s", directive, matches[0][1], m[1])
		}
	}
	n, _ := strconv.Atoi(matches[0][1])
	return time.Duration(n) * time.Second
}

// TestTimeLimitsFireInnermostFirst checks the chain of time limits a
// request crosses: each inner one must fire first, so a slow request is
// answered by the API's JSON and never by nginx's HTML page.
func TestTimeLimitsFireInnermostFirst(t *testing.T) {
	conf := readNginxConf(t)
	srv := newHTTPServer(":0", http.NotFoundHandler())
	chain := []struct {
		name string
		d    time.Duration
	}{
		{"postgres.RequestStatementTimeout", postgres.RequestStatementTimeout},
		{"httpadapter.RequestDeadline", httpadapter.RequestDeadline},
		{"http.Server WriteTimeout", srv.WriteTimeout},
		{"nginx proxy_read_timeout", nginxSeconds(t, conf, "proxy_read_timeout", 3)},
	}
	for i := 1; i < len(chain); i++ {
		if chain[i-1].d >= chain[i].d {
			t.Errorf("%s (%v) must be shorter than %s (%v)", chain[i-1].name, chain[i-1].d, chain[i].name, chain[i].d)
		}
	}
}

// TestNginxDropsIdleConnectionsBeforeTheAPIDoes pins the keepalive pairing:
// if the API closed an idle connection first, nginx could send a request
// down one that is already closing.
func TestNginxDropsIdleConnectionsBeforeTheAPIDoes(t *testing.T) {
	conf := readNginxConf(t)
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.IdleTimeout == 0 {
		t.Fatal("IdleTimeout is unset; net/http would fall back to ReadTimeout")
	}
	if keep := nginxSeconds(t, conf, "keepalive_timeout", 1); keep >= srv.IdleTimeout {
		t.Fatalf("nginx keepalive_timeout (%v) must be shorter than the API's IdleTimeout (%v)", keep, srv.IdleTimeout)
	}
}

// Every location that proxies to the API must reuse connections the same
// way, or one location silently opens a new connection per request.
func TestEveryAPILocationReusesConnections(t *testing.T) {
	conf := readNginxConf(t)
	for directive, want := range map[string]int{
		`proxy_pass http://api;`:          3,
		`proxy_http_version 1.1;`:         3,
		`proxy_set_header Connection "";`: 3,
		`keepalive 16;`:                   1,
	} {
		if got := regexp.MustCompile(regexp.QuoteMeta(directive)).FindAllStringIndex(conf, -1); len(got) != want {
			t.Errorf("%q appears %d times in web/nginx.conf, want %d", directive, len(got), want)
		}
	}
}
