package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestATokenCredentialSendsBearerAndNoCookiesOrCSRF(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Token: "hearth_abc"})

	if _, _, err := run_(t, srv.URL, "", "category", "add", "--name=Pets"); err != nil {
		t.Fatal(err)
	}
	r := f.seen[0]
	if r.Header.Get("Authorization") != "Bearer hearth_abc" {
		t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
	}
	if len(r.Cookies()) != 0 || r.Header.Get("X-CSRF-Token") != "" {
		t.Fatalf("a token request must carry no cookies and no CSRF header: cookies=%v csrf=%q", r.Cookies(), r.Header.Get("X-CSRF-Token"))
	}
}

func TestLoginWithTokenProvesItBeforeStoring(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	t.Setenv("HEARTH_TOKEN", "hearth_bad")
	f, srv := newFakeAPI(t)
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer hearth_good" {
			w.Write([]byte(`{"user":{"email":"a@example.com"}}`))
			return
		}
		http.Error(w, `{"error":{"code":"UNAUTHENTICATED"}}`, 401)
	}
	_, _, err := run_(t, srv.URL, "", "login", "--token")
	if exitCode(t, err) != exitSignInAgain {
		t.Fatalf("a refused token must exit 2, got %v", err)
	}
	st, _ := newStore(srv.URL)
	if _, err := st.load(); err == nil {
		t.Fatalf("a refused token must not be stored")
	}

	t.Setenv("HEARTH_TOKEN", "hearth_good")
	if _, _, err := run_(t, srv.URL, "", "login", "--token"); err != nil {
		t.Fatal(err)
	}
	creds, err := st.load()
	if err != nil || creds.Token != "hearth_good" || creds.Email != "a@example.com" || creds.Session != "" {
		t.Fatalf("stored %+v, %v", creds, err)
	}
}

func TestTokenCreateNeedsASessionNotAToken(t *testing.T) {
	t.Setenv("HEARTH_CONFIG_DIR", t.TempDir())
	f, srv := newFakeAPI(t)
	st, _ := newStore(srv.URL)
	st.save(&credentials{BaseURL: srv.URL, Token: "hearth_abc"})
	_, _, err := run_(t, srv.URL, "", "token", "create", "--name=x")
	if exitCode(t, err) != exitUsage || len(f.seen) != 0 {
		t.Fatalf("expected a client-side refusal with no request, got %v (%d requests)", err, len(f.seen))
	}

	st.save(&credentials{BaseURL: srv.URL, Session: "s", CSRF: "c"})
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte(`{"id":"t1","name":"x","prefix":"abcdefgh","token":"hearth_abcdefgh..."}`))
	}
	out, errOut, err := run_(t, srv.URL, "", "token", "create", "--name=x", "--days=30")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"token":"hearth_`) || !strings.Contains(errOut, "only time") {
		t.Fatalf("create should print the token once and warn: out=%q err=%q", out, errOut)
	}
	if !strings.Contains(string(f.bodies[len(f.bodies)-1]), `"expiresInDays":30`) {
		t.Fatalf("days not sent: %s", f.bodies[len(f.bodies)-1])
	}
}
