package main

import (
	"io"
	"reflect"
	"testing"
)

// The wire field names here are the ones the handlers decode. If a handler
// renames a field, this test and the handler's own request struct disagree
// -- which is the point: the CLI must not silently post a key the server
// ignores.
func TestTypedInsertsBuildTheExactWireBody(t *testing.T) {
	cases := []struct {
		kind string
		args []string
		want map[string]any
	}{
		{
			"transaction",
			[]string{"--kind=expense", "--date=2026-09-08", "--description=Coffee", "--amount-minor=650", "--from-account=a1", "--category=c1", "--paid-by=m1"},
			map[string]any{"kind": "expense", "occurredOn": "2026-09-08", "description": "Coffee", "amountMinor": int64(650), "fromAccountId": "a1", "categoryId": "c1", "paidByMembershipId": "m1"},
		},
		{
			"transaction",
			[]string{"--kind=transfer", "--date=2026-09-08", "--description=Move", "--amount-minor=10000", "--from-account=a1", "--to-account=a2", "--received-minor=7300"},
			map[string]any{"kind": "transfer", "occurredOn": "2026-09-08", "description": "Move", "amountMinor": int64(10000), "fromAccountId": "a1", "toAccountId": "a2", "receivedAmountMinor": int64(7300)},
		},
		{
			"account",
			[]string{"--nickname=DBS", "--type=cash", "--currency=SGD", "--as-of=2026-01-01", "--opening-balance-minor=500000"},
			map[string]any{"nickname": "DBS", "type": "cash", "openingBalanceMinor": int64(500000), "openingBalanceCurrency": "SGD", "openingBalanceAsOf": "2026-01-01", "countTowardNetWorth": true, "visibleToLimitedMembers": true},
		},
		{
			"bill",
			[]string{"--name=Rent", "--amount-minor=250000", "--cadence=monthly", "--next-due=2026-10-01", "--category=c1", "--pay-from-account=a1", "--paid-by=m1", "--autopay"},
			map[string]any{"name": "Rent", "amountMinor": int64(250000), "cadence": "monthly", "nextDue": "2026-10-01", "categoryId": "c1", "payFromAccountId": "a1", "paidByMembershipId": "m1", "autopay": true, "isSubscription": false},
		},
		{
			"goal",
			[]string{"--name=Japan", "--target-minor=800000", "--currency=SGD", "--target-month=2027-03", "--planned-monthly-minor=100000"},
			map[string]any{"name": "Japan", "targetMinor": int64(800000), "currency": "SGD", "targetMonth": "2027-03", "plannedMonthlyMinor": int64(100000), "startingBalanceMinor": int64(0)},
		},
		{
			"category",
			[]string{"--name=Groceries"},
			map[string]any{"name": "Groceries"},
		},
	}
	for _, tc := range cases {
		got, err := inserts[tc.kind].build(tc.args, io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", tc.kind, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %v\nwant %v", tc.kind, got, tc.want)
		}
	}
}

func TestTypedInsertsRefuseWhatTheServerWouldRefuse(t *testing.T) {
	bad := map[string][]string{
		"an expense with no source account": {"--kind=expense", "--date=2026-09-08", "--description=x", "--amount-minor=1"},
		"an income with no destination":     {"--kind=income", "--date=2026-09-08", "--description=x", "--amount-minor=1"},
		"a transfer with one side":          {"--kind=transfer", "--date=2026-09-08", "--description=x", "--amount-minor=1", "--from-account=a"},
		"an unknown kind":                   {"--kind=refund", "--date=2026-09-08", "--description=x", "--amount-minor=1", "--to-account=a"},
		"a zero amount":                     {"--kind=expense", "--date=2026-09-08", "--description=x", "--amount-minor=0", "--from-account=a"},
		"a stray positional argument":       {"--kind=expense", "--date=2026-09-08", "--description=x", "--amount-minor=1", "--from-account=a", "extra"},
	}
	for name, args := range bad {
		_, err := buildTransaction(args, io.Discard)
		if exitCode(t, err) != exitUsage {
			t.Errorf("%s: expected a usage error, got %v", name, err)
		}
	}
}
