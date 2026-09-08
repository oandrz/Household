package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"
)

// cmdLogin signs in with email and password and stores the session.
//
// The password is read from $HEARTH_PASSWORD or from stdin, never from a
// flag: a flag lands in shell history and in `ps` output. On a terminal the
// prompt hides what is typed; piped in, one line is read so
// `printf '%s' "$PW" | hearthctl login --email=...` works from a script.
//
// It never retries. Five wrong passwords lock the whole household out
// (domain.DefaultLockoutPolicy), and an agent that loops on a bad password
// would do exactly that. One attempt, one answer.
func cmdLogin(ctx context.Context, c *client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "the member's email address (required)")
	if err := fs.Parse(args); err != nil {
		return fail(exitUsage, "")
	}
	if *email == "" {
		return fail(exitUsage, "login needs --email")
	}

	password, err := readPassword(stdin, stderr)
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]string{"email": *email, "password": password})
	// Sign-in must not carry a stale session: the server ignores it, but a
	// stale CSRF cookie next to no header is exactly what requireCSRF
	// rejects on routes that check it, and clearing first keeps the request
	// identical to a browser's first sign-in.
	c.creds = nil
	res, err := c.do(ctx, http.MethodPost, "/auth/sign-in", body)
	if err != nil {
		return err
	}
	if err := refuseSignIn(res, stdout); err != nil {
		return err
	}
	if c.creds == nil || c.creds.Session == "" {
		return fail(exitAPIRefused, "the API answered 200 but set no session cookie")
	}
	c.creds.Email = *email
	if err := c.store.save(c.creds); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "signed in to %s as %s; session stored in %s (expires %s)\n",
		c.baseURL, *email, c.store.path, c.creds.ExpiresAt.Format("2006-01-02"))
	writeBody(stdout, res.body)
	return nil
}

func readPassword(stdin io.Reader, stderr io.Writer) (string, error) {
	if pw := os.Getenv("HEARTH_PASSWORD"); pw != "" {
		return pw, nil
	}
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stderr, "password: ")
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", fail(exitUsage, "reading password: %v", err)
		}
		return string(raw), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fail(exitUsage, "no password: set HEARTH_PASSWORD or pipe it on stdin")
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return "", fail(exitUsage, "no password: set HEARTH_PASSWORD or pipe it on stdin")
	}
	return pw, nil
}

// cmdLogout revokes the session server-side and forgets the file. The file
// is cleared even if the server refuses (a session already expired answers
// 401): the stored value is useless either way.
func cmdLogout(ctx context.Context, c *client, stdout, stderr io.Writer) error {
	if err := c.requireCreds(); err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodPost, "/auth/sign-out", nil)
	if clearErr := c.store.clear(); clearErr != nil {
		return clearErr
	}
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "signed out of %s\n", c.baseURL)
	writeBody(stdout, res.body)
	return nil
}

// cmdWhoami prints the /auth/me bundle: the user, household, membership
// (whose id the bill and transaction inserts take as paidBy), capabilities
// and feature flags. It is also the cheapest "is my session still good"
// check an agent can make.
func cmdWhoami(ctx context.Context, c *client, stdout io.Writer) error {
	if err := c.requireCreds(); err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodGet, "/auth/me", nil)
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	writeBody(stdout, res.body)
	return nil
}
