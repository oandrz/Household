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
	email := fs.String("email", "", "the member's email address")
	useToken := fs.Bool("token", false, "sign in with a personal API token from $HEARTH_TOKEN or stdin instead of a password")
	if err := fs.Parse(args); err != nil {
		return fail(exitUsage, "")
	}
	if *useToken {
		return loginWithToken(ctx, c, stdin, stdout, stderr)
	}
	if *email == "" {
		return fail(exitUsage, "login needs --email (or --token)")
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

// loginWithToken stores a personal API token as the credential. The token
// is read the way a password is -- environment or stdin, never a flag -- and
// proven against /auth/me before it is written, so a mistyped token is
// refused now rather than on the first real command.
func loginWithToken(ctx context.Context, c *client, stdin io.Reader, stdout, stderr io.Writer) error {
	raw := os.Getenv("HEARTH_TOKEN")
	if raw == "" {
		var err error
		raw, err = readSecret(stdin, stderr, "token: ", "no token: set HEARTH_TOKEN or pipe it on stdin")
		if err != nil {
			return err
		}
	}
	c.creds = &credentials{BaseURL: c.baseURL, Token: raw}
	res, err := c.do(ctx, http.MethodGet, "/auth/me", nil)
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	json.Unmarshal(res.body, &me)
	c.creds.Email = me.User.Email
	if err := c.store.save(c.creds); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "token accepted for %s on %s; stored in %s\n", me.User.Email, c.baseURL, c.store.path)
	writeBody(stdout, res.body)
	return nil
}

func readPassword(stdin io.Reader, stderr io.Writer) (string, error) {
	if pw := os.Getenv("HEARTH_PASSWORD"); pw != "" {
		return pw, nil
	}
	return readSecret(stdin, stderr, "password: ", "no password: set HEARTH_PASSWORD or pipe it on stdin")
}

// readSecret reads one secret from a terminal (hidden) or one line from a
// pipe. Shared by the password and the token so neither ever becomes a
// flag that lands in shell history.
func readSecret(stdin io.Reader, stderr io.Writer, prompt, missing string) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stderr, prompt)
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", fail(exitUsage, "reading secret: %v", err)
		}
		return string(raw), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fail(exitUsage, "%s", missing)
	}
	secret := strings.TrimRight(line, "\r\n")
	if secret == "" {
		return "", fail(exitUsage, "%s", missing)
	}
	return secret, nil
}

// cmdLogout revokes the session server-side and forgets the file. The file
// is cleared even if the server refuses (a session already expired answers
// 401): the stored value is useless either way. A stored token is only
// forgotten -- revoking it is `hearthctl token revoke`, from a browser
// session, because a token cannot revoke a token.
func cmdLogout(ctx context.Context, c *client, stdout, stderr io.Writer) error {
	if err := c.requireCreds(); err != nil {
		return err
	}
	if c.creds.Token != "" {
		if err := c.store.clear(); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "forgot the stored token for %s (it is still valid; revoke it with: hearthctl token revoke <id>)\n", c.baseURL)
		return nil
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
