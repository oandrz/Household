package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
)

// cmdAPI is the escape hatch: any method, any path, an optional JSON body.
// It is what makes every route reachable without a typed verb for each,
// and what an agent uses for anything `routes` lists that has no verb.
func cmdAPI(ctx context.Context, c *client, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	data := fs.String("data", "", "JSON request body, or @path to read it from a file")
	// Flags may come before or after the two positionals.
	positional, flagArgs := splitPositionals(args, 2)
	if err := fs.Parse(flagArgs); err != nil {
		return fail(exitUsage, "api: %v", err)
	}
	if len(positional) != 2 {
		return fail(exitUsage, "usage: hearthctl api <METHOD> <path> [--data=<json>|--data=@file]")
	}
	method := strings.ToUpper(positional[0])
	path := positional[1]

	var body []byte
	if *data != "" {
		raw := []byte(*data)
		if strings.HasPrefix(*data, "@") {
			var err error
			raw, err = os.ReadFile((*data)[1:])
			if err != nil {
				return fail(exitUsage, "reading --data file: %v", err)
			}
		}
		if !json.Valid(raw) {
			return fail(exitUsage, "--data is not valid JSON")
		}
		body = raw
	}

	if err := c.requireCreds(); err != nil && !isPublic(method, path) {
		return err
	}
	res, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	writeBody(stdout, res.body)
	return nil
}

// isPublic names the few routes that work without a session, so `api` can
// reach them before login. Everything else short-circuits with exit 2
// client-side rather than spending a round trip to be told 401.
func isPublic(method, path string) bool {
	path = strings.TrimPrefix(path, apiPrefix)
	switch {
	case method == http.MethodGet && path == "/currencies":
		return true
	case path == "/auth/sign-in", strings.HasPrefix(path, "/auth/sign-up"), strings.HasPrefix(path, "/auth/magic-link"):
		return true
	case strings.HasPrefix(path, "/invites/"):
		return true
	}
	return false
}

// splitPositionals separates the first n non-flag arguments from everything
// else, so `api POST /x --data=...` and `api --data=... POST /x` both work.
func splitPositionals(args []string, n int) (positional, flags []string) {
	for _, a := range args {
		if len(positional) < n && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
	}
	return positional, flags
}

// listPaths maps the kinds `list` accepts to the read route each lives at.
// Members is here because a bill's paidBy and a transaction's paidBy are
// membership ids, and an agent has to look those up before it can insert.
var listPaths = map[string]string{
	"accounts":     "/accounts",
	"categories":   "/categories",
	"members":      "/household/members",
	"goals":        "/goals",
	"bills":        "/bills",
	"transactions": "/transactions",
}

func cmdList(ctx context.Context, c *client, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fail(exitUsage, "usage: hearthctl list <accounts|categories|members|goals|bills|transactions>")
	}
	path, ok := listPaths[args[0]]
	if !ok {
		return fail(exitUsage, "list: unknown kind %q", args[0])
	}
	if err := c.requireCreds(); err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	writeBody(stdout, res.body)
	return nil
}
