package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialFileIsOwnerOnlyAndKeyedByHost(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEARTH_CONFIG_DIR", dir)

	local, _ := newStore("http://localhost:8080")
	prod, _ := newStore("https://oink.mywire.org")
	if local.path == prod.path {
		t.Fatalf("two hosts must not share a file: %s", local.path)
	}
	if filepath.Base(local.path) != "localhost_8080.json" {
		t.Fatalf("unexpected name %s", local.path)
	}

	if err := local.save(&credentials{Session: "s", CSRF: "c"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(local.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode %o, want 0600", info.Mode().Perm())
	}
	if _, err := prod.load(); err == nil {
		t.Fatalf("the production store must not see localhost's session")
	}
}

func TestStoreRefusesARelativeURL(t *testing.T) {
	if _, err := newStore("localhost:8080"); err == nil {
		t.Fatalf("a URL with no scheme has no host to key on and must be refused")
	}
}
