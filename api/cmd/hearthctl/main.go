// Command hearthctl is Hearth's command-line client: the way a script, a
// cron job or an AI agent drives the product without a browser.
//
// It is a plain HTTP client of the same API the frontend uses. It signs in
// the way the browser does, keeps the two cookies the server issues, and
// sends the CSRF cookie back as a header on every write. It deliberately
// imports nothing from internal/: going around the HTTP layer would go
// around the only place authorisation lives (see CLAUDE.md), and coupling a
// client binary to server internals defeats the point of having an API.
//
// See docs/CLI.md for how to use it and docs/adr/0006 for why it is shaped
// this way.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// Exit codes are part of the contract: a program calling hearthctl decides
// what to do next from the code, not by parsing stderr.
const (
	exitOK          = 0
	exitUsage       = 1
	exitSignInAgain = 2 // the API answered 401: the stored session is gone
	exitAPIRefused  = 3 // any other 4xx/5xx; the body was printed to stdout
	exitUnreachable = 4 // could not talk to the server at all
)

// exitError carries an exit code alongside the message. main prints the
// message to stderr and exits with the code; nothing else in the tool calls
// os.Exit, so every path stays testable.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func fail(code int, format string, args ...any) error {
	return &exitError{code: code, msg: fmt.Sprintf(format, args...)}
}

const usage = `usage: hearthctl [--url=<base>] <command> [flags]

  --url        API base URL (default $HEARTH_URL, then http://localhost:8080)

commands:
  login --email=<email>        sign in; password from $HEARTH_PASSWORD or stdin
  logout                       sign out and forget the stored session
  whoami                       print the signed-in user, household, membership
  routes [--json]              every API route, who may call it, its body shape
  list <kind>                  accounts | categories | members | goals | bills |
                               transactions
  api <METHOD> <path> [--data=<json>|--data=@file]
                               call any route; path is relative to /api/v1
  transaction add ...          write an expense, income or transfer (--key makes it retry-safe)
  transaction import <file.csv> [--dry-run]
                               many rows, each with an idempotency key, so the
                               same file run twice creates nothing new
  account add ...              create an account
  bill add ...                 create a recurring bill
  goal add ...                 create a savings goal
  category add --name=<name>   create a spending category

Response JSON goes to stdout. Messages for a person go to stderr.
Exit codes: 0 ok, 1 usage, 2 sign in again, 3 the API refused, 4 unreachable.
Writes need an owner's session; a limited member gets 403 on every insert.
`

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		os.Exit(exitOK)
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintln(os.Stderr, "hearthctl:", ee.msg)
		}
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, "hearthctl:", err)
	os.Exit(exitUsage)
}

// run is the whole program behind os.Exit. Every command receives the same
// three streams so tests can drive it end to end against a fake server.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	baseURL, rest, err := splitGlobalFlags(args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fail(exitUsage, "%s", usage)
	}

	store, err := newStore(baseURL)
	if err != nil {
		return err
	}
	client := newClient(baseURL, store)

	switch rest[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "login":
		return cmdLogin(ctx, client, rest[1:], stdin, stdout, stderr)
	case "logout":
		return cmdLogout(ctx, client, stdout, stderr)
	case "whoami":
		return cmdWhoami(ctx, client, stdout)
	case "routes":
		return cmdRoutes(rest[1:], stdout)
	case "list":
		return cmdList(ctx, client, rest[1:], stdout)
	case "api":
		return cmdAPI(ctx, client, rest[1:], stdout)
	case "transaction", "account", "bill", "goal", "category":
		if rest[0] == "transaction" && len(rest) > 1 && rest[1] == "import" {
			return cmdImport(ctx, client, rest[2:], stdout, stderr)
		}
		return cmdAdd(ctx, client, rest[0], rest[1:], stdout)
	default:
		return fail(exitUsage, "unknown command %q\n\n%s", rest[0], usage)
	}
}

// splitGlobalFlags peels --url off the front so every command sees only its
// own flags. The default chain is flag, then $HEARTH_URL, then localhost:8080
// -- the Go server's port, not Vite's 5173.
func splitGlobalFlags(args []string) (baseURL string, rest []string, err error) {
	baseURL = os.Getenv("HEARTH_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	for len(args) > 0 {
		switch {
		case args[0] == "--url":
			if len(args) < 2 {
				return "", nil, fail(exitUsage, "--url needs a value")
			}
			baseURL, args = args[1], args[2:]
		case len(args[0]) > 6 && args[0][:6] == "--url=":
			baseURL, args = args[0][6:], args[1:]
		default:
			return baseURL, args, nil
		}
	}
	return baseURL, args, nil
}
