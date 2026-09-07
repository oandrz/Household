package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
)

// The typed inserts. Each one builds exactly the JSON the matching handler
// decodes -- the field names are copied from the request structs in
// internal/adapter/http/*_handlers.go and pinned by cmd_add_test.go. Amounts
// are minor units on the command line as they are on the wire
// (--amount-minor=1234 is 12.34), so no decimal parsing and no float64
// anywhere in this tool.
//
// An optional string flag left empty is omitted from the body rather than
// sent as "", because the handlers take pointers for optional ids and treat
// an empty string as a present-but-empty id, which the service refuses.

// insert is one typed verb: which route it posts to and how to turn flags
// into a body. Keeping the two together is what lets cmdAdd stay generic.
type insert struct {
	path  string
	build func(args []string, stderr io.Writer) (map[string]any, error)
}

// keyFlag is the one flag that is not part of the body: it becomes the
// Idempotency-Key header. Only transactions honour it server-side today, so
// only transaction add declares it (buildTransaction pops it off the body
// under this name before the body is sent).
const keyFlag = "__idempotencyKey"

var inserts = map[string]insert{
	"transaction": {path: "/transactions", build: buildTransaction},
	"account":     {path: "/accounts", build: buildAccount},
	"bill":        {path: "/bills", build: buildBill},
	"goal":        {path: "/goals", build: buildGoal},
	"category":    {path: "/categories", build: buildCategory},
}

func cmdAdd(ctx context.Context, c *client, kind string, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "add" {
		return fail(exitUsage, "usage: hearthctl %s add [flags]  (run with -h for the flags)", kind)
	}
	ins := inserts[kind]
	body, err := ins.build(args[1:], io.Discard)
	if err != nil {
		return err
	}
	if err := c.requireCreds(); err != nil {
		return err
	}
	var headers map[string]string
	if key, ok := body[keyFlag].(string); ok {
		delete(body, keyFlag)
		if key != "" {
			headers = map[string]string{idempotencyKeyHeader: key}
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := c.doWithHeaders(ctx, http.MethodPost, ins.path, raw, headers)
	if err != nil {
		return err
	}
	if err := refuse(res, stdout); err != nil {
		return err
	}
	writeBody(stdout, res.body)
	return nil
}

// newFlags builds a flag set whose parse failure is a usage error that
// shows the flag's own help, so `hearthctl bill add -h` is self-describing.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return fail(exitUsage, "")
		}
		return fail(exitUsage, "%s: %v", fs.Name(), err)
	}
	if fs.NArg() != 0 {
		return fail(exitUsage, "%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
}

func need(fs *flag.FlagSet, pairs ...string) error {
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			return fail(exitUsage, "%s needs --%s", fs.Name(), pairs[i])
		}
	}
	return nil
}

// setIf adds an optional string only when it was given.
func setIf(body map[string]any, key, value string) {
	if value != "" {
		body[key] = value
	}
}

func buildTransaction(args []string, stderr io.Writer) (map[string]any, error) {
	fs := newFlags("transaction add", stderr)
	kind := fs.String("kind", "", "expense | income | transfer (required)")
	date := fs.String("date", "", "the day it happened, YYYY-MM-DD (required)")
	desc := fs.String("description", "", "what it was (required)")
	amount := fs.Int64("amount-minor", 0, "amount in minor units, e.g. 1234 for 12.34 (required, > 0)")
	from := fs.String("from-account", "", "account id money left (expense and transfer)")
	to := fs.String("to-account", "", "account id money arrived in (income and transfer)")
	category := fs.String("category", "", "category id (expense and income)")
	paidBy := fs.String("paid-by", "", "membership id of who paid (see whoami / list members)")
	received := fs.Int64("received-minor", -1, "amount that arrived, minor units, for a transfer between currencies")
	key := fs.String("key", "", "idempotency key: the same key with the same fields never creates a second row, so a retry is safe")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if err := need(fs, "kind", *kind, "date", *date, "description", *desc); err != nil {
		return nil, err
	}
	if *amount <= 0 {
		return nil, fail(exitUsage, "transaction add needs --amount-minor greater than 0")
	}
	switch *kind {
	case "expense":
		if *from == "" {
			return nil, fail(exitUsage, "an expense needs --from-account")
		}
	case "income":
		if *to == "" {
			return nil, fail(exitUsage, "an income needs --to-account")
		}
	case "transfer":
		if *from == "" || *to == "" {
			return nil, fail(exitUsage, "a transfer needs both --from-account and --to-account")
		}
	default:
		return nil, fail(exitUsage, "--kind must be expense, income or transfer, got %q", *kind)
	}
	body := map[string]any{
		"kind":        *kind,
		"occurredOn":  *date,
		"description": *desc,
		"amountMinor": *amount,
	}
	setIf(body, "fromAccountId", *from)
	setIf(body, "toAccountId", *to)
	setIf(body, "categoryId", *category)
	setIf(body, "paidByMembershipId", *paidBy)
	if *received >= 0 {
		body["receivedAmountMinor"] = *received
	}
	if *key != "" {
		body[keyFlag] = *key
	}
	return body, nil
}

func buildAccount(args []string, stderr io.Writer) (map[string]any, error) {
	fs := newFlags("account add", stderr)
	nickname := fs.String("nickname", "", "the account's name (required)")
	typ := fs.String("type", "", "cash | investment | property | loan | credit_card (required)")
	opening := fs.Int64("opening-balance-minor", 0, "starting balance in minor units (may be 0)")
	currency := fs.String("currency", "", "ISO 4217 code, e.g. SGD (required)")
	asOf := fs.String("as-of", "", "the day the starting balance is true, YYYY-MM-DD (required)")
	owner := fs.String("owner", "", "membership id of the account's owner; omit for a joint account")
	netWorth := fs.Bool("count-toward-net-worth", true, "include in net worth")
	visible := fs.Bool("visible-to-limited", true, "limited members may see this account's name")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if err := need(fs, "nickname", *nickname, "type", *typ, "currency", *currency, "as-of", *asOf); err != nil {
		return nil, err
	}
	body := map[string]any{
		"nickname":                *nickname,
		"type":                    *typ,
		"openingBalanceMinor":     *opening,
		"openingBalanceCurrency":  *currency,
		"openingBalanceAsOf":      *asOf,
		"countTowardNetWorth":     *netWorth,
		"visibleToLimitedMembers": *visible,
	}
	setIf(body, "ownerMembershipId", *owner)
	return body, nil
}

func buildBill(args []string, stderr io.Writer) (map[string]any, error) {
	fs := newFlags("bill add", stderr)
	name := fs.String("name", "", "the bill's name (required)")
	amount := fs.Int64("amount-minor", 0, "amount in minor units (required, > 0)")
	cadence := fs.String("cadence", "", "one_off | monthly | quarterly | yearly (required)")
	nextDue := fs.String("next-due", "", "next due date, YYYY-MM-DD (required)")
	category := fs.String("category", "", "category id (required)")
	payFrom := fs.String("pay-from-account", "", "account id it is paid from (required)")
	paidBy := fs.String("paid-by", "", "membership id of who pays it (required)")
	autopay := fs.Bool("autopay", false, "paid automatically")
	subscription := fs.Bool("subscription", false, "counts in the subscriptions rollup")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if err := need(fs, "name", *name, "cadence", *cadence, "next-due", *nextDue,
		"category", *category, "pay-from-account", *payFrom, "paid-by", *paidBy); err != nil {
		return nil, err
	}
	if *amount <= 0 {
		return nil, fail(exitUsage, "bill add needs --amount-minor greater than 0")
	}
	return map[string]any{
		"name":               *name,
		"amountMinor":        *amount,
		"cadence":            *cadence,
		"nextDue":            *nextDue,
		"categoryId":         *category,
		"payFromAccountId":   *payFrom,
		"paidByMembershipId": *paidBy,
		"autopay":            *autopay,
		"isSubscription":     *subscription,
	}, nil
}

func buildGoal(args []string, stderr io.Writer) (map[string]any, error) {
	fs := newFlags("goal add", stderr)
	name := fs.String("name", "", "the goal's name (required)")
	target := fs.Int64("target-minor", 0, "target in minor units (required, > 0)")
	currency := fs.String("currency", "", "ISO 4217 code (required)")
	month := fs.String("target-month", "", "when to reach it, YYYY-MM (optional)")
	planned := fs.Int64("planned-monthly-minor", 0, "planned monthly contribution, minor units")
	starting := fs.Int64("starting-balance-minor", 0, "already saved toward it, minor units")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if err := need(fs, "name", *name, "currency", *currency); err != nil {
		return nil, err
	}
	if *target <= 0 {
		return nil, fail(exitUsage, "goal add needs --target-minor greater than 0")
	}
	body := map[string]any{
		"name":                 *name,
		"targetMinor":          *target,
		"currency":             *currency,
		"plannedMonthlyMinor":  *planned,
		"startingBalanceMinor": *starting,
	}
	setIf(body, "targetMonth", *month)
	return body, nil
}

func buildCategory(args []string, stderr io.Writer) (map[string]any, error) {
	fs := newFlags("category add", stderr)
	name := fs.String("name", "", "the category's name (required)")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if err := need(fs, "name", *name); err != nil {
		return nil, err
	}
	return map[string]any{"name": *name}, nil
}
