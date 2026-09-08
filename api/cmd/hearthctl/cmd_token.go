package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// token create / list / revoke. Creating and revoking need a browser
// session (a token cannot mint or revoke a token, server rule), so these
// are run right after `hearthctl login --email=...`; the token is then used
// on the headless machine with `hearthctl login --token`.
func cmdToken(ctx context.Context, c *client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fail(exitUsage, "usage: hearthctl token <create --name=<n> [--days=90] | list | revoke <id>>")
	}
	if err := c.requireCreds(); err != nil {
		return err
	}
	switch args[0] {
	case "create":
		fs := newFlags("token create", stderr)
		name := fs.String("name", "", "what this token is for, e.g. \"laptop cron\" (required)")
		days := fs.Int("days", 0, "lifetime in days (default 90, at most 365)")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}
		if err := need(fs, "name", *name); err != nil {
			return err
		}
		if c.creds.Token != "" {
			return fail(exitUsage, "a token cannot create a token: sign in with --email first")
		}
		body, _ := json.Marshal(map[string]any{"name": *name, "expiresInDays": *days})
		res, err := c.do(ctx, http.MethodPost, "/auth/tokens", body)
		if err != nil {
			return err
		}
		if err := refuse(res, stdout); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "This is the only time the token is shown. Store it; use it with: HEARTH_TOKEN=<token> hearthctl login --token")
		writeBody(stdout, res.body)
		return nil
	case "list":
		res, err := c.do(ctx, http.MethodGet, "/auth/tokens", nil)
		if err != nil {
			return err
		}
		if err := refuse(res, stdout); err != nil {
			return err
		}
		writeBody(stdout, res.body)
		return nil
	case "revoke":
		if len(args) != 2 {
			return fail(exitUsage, "usage: hearthctl token revoke <id>")
		}
		if c.creds.Token != "" {
			return fail(exitUsage, "a token cannot revoke a token: sign in with --email first")
		}
		res, err := c.do(ctx, http.MethodDelete, "/auth/tokens/"+args[1], nil)
		if err != nil {
			return err
		}
		if err := refuse(res, stdout); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "revoked %s\n", args[1])
		return nil
	default:
		return fail(exitUsage, "token: unknown subcommand %q", args[0])
	}
}
