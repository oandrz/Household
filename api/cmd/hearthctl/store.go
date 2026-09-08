package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// credentials is what the store keeps between runs: the two cookie values
// the server issued, when the session lapses, and who it belongs to. The
// session value is a bearer secret -- whoever holds the file is signed in --
// which is why the file is written 0600 and why login never puts the
// password itself anywhere.
type credentials struct {
	BaseURL   string    `json:"baseUrl"`
	Email     string    `json:"email"`
	Session   string    `json:"session"`
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// store is the credential file for one base URL. One file per host means a
// localhost session and the production one can never be confused for each
// other -- a write meant for the dev stack cannot land in a real household
// because the last login happened to be there.
type store struct {
	path string
}

// newStore derives the file path from the base URL's host and port. The
// directory is $HEARTH_CONFIG_DIR, then $XDG_CONFIG_HOME/hearth, then
// ~/.config/hearth. HEARTH_CONFIG_DIR exists so tests and sandboxes never
// touch the real home directory.
func newStore(baseURL string) (*store, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fail(exitUsage, "--url must be an absolute URL like http://localhost:8080, got %q", baseURL)
	}
	dir := os.Getenv("HEARTH_CONFIG_DIR")
	if dir == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fail(exitUsage, "cannot find a home directory for the credential file: %v", err)
			}
			base = filepath.Join(home, ".config")
		}
		dir = filepath.Join(base, "hearth")
	}
	// Host and port are file-name safe apart from the colon.
	name := strings.ReplaceAll(u.Host, ":", "_") + ".json"
	return &store{path: filepath.Join(dir, name)}, nil
}

func (s *store) load() (*credentials, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var c credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("credential file %s is not valid JSON: %w", s.path, err)
	}
	if c.Session == "" {
		return nil, errors.New("credential file has no session")
	}
	return &c, nil
}

// save writes the file with owner-only permissions, creating the directory
// on first use. It writes to a temp file and renames so a crash mid-write
// cannot leave a half-written file that load would then refuse forever.
func (s *store) save(c *credentials) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fail(exitUsage, "creating %s: %v", filepath.Dir(s.path), err)
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fail(exitUsage, "writing %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fail(exitUsage, "writing %s: %v", s.path, err)
	}
	return nil
}

// clear forgets the session. A file that is already gone is not an error:
// the outcome "no stored session" is the same either way.
func (s *store) clear() error {
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(exitUsage, "removing %s: %v", s.path, err)
	}
	return nil
}
