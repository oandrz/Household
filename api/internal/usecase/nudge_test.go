package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type nudgeRepoStub struct {
	recipients []usecase.NudgeRecipient
	claims     map[string]bool
	released   []string
}

func key(chat int64, hh string, day time.Time) string {
	return day.Format("2006-01-02") + "/" + hh + "/" + string(rune(chat))
}

func (r *nudgeRepoStub) Recipients(context.Context) ([]usecase.NudgeRecipient, error) {
	return r.recipients, nil
}
func (r *nudgeRepoStub) Claim(_ context.Context, chat int64, hh string, day time.Time) (bool, error) {
	if r.claims == nil {
		r.claims = map[string]bool{}
	}
	k := key(chat, hh, day)
	if r.claims[k] {
		return false, nil
	}
	r.claims[k] = true
	return true, nil
}
func (r *nudgeRepoStub) Release(_ context.Context, chat int64, hh string, day time.Time) error {
	k := key(chat, hh, day)
	delete(r.claims, k)
	r.released = append(r.released, k)
	return nil
}
func (r *nudgeRepoStub) SetEnabled(context.Context, int64, bool) error   { return nil }
func (r *nudgeRepoStub) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

type billsStub struct{ view usecase.BillsView }

func (b billsStub) List(context.Context, string, bool, time.Time) (usecase.BillsView, error) {
	return b.view, nil
}

type budgetStub struct {
	view  usecase.BudgetMonthView
	asked []time.Time
}

func (b *budgetStub) Month(_ context.Context, _ string, month, _ time.Time) (usecase.BudgetMonthView, error) {
	b.asked = append(b.asked, month)
	return b.view, nil
}

type nudgeSenderSpy struct {
	sent []string
	fail bool
}

func (s *nudgeSenderSpy) SendMessage(_ context.Context, _ int64, text string) error {
	if s.fail {
		return errors.New("telegram down")
	}
	s.sent = append(s.sent, text)
	return nil
}

var nudgeToday = time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)

func nudgeBill(name string, due time.Time, autopay bool, overdue bool) usecase.BillView {
	d := due
	return usecase.BillView{Bill: domain.Bill{Name: name, Amount: domain.Money{Amount: 12345, Currency: "SGD"}, NextDue: &d, Autopay: autopay}, Overdue: overdue}
}

func TestComposeNamesBillsWithinThreeDaysAndBudgetLinesAtEightyPercent(t *testing.T) {
	bills := billsStub{view: usecase.BillsView{Bills: []usecase.BillView{
		nudgeBill("Rent", nudgeToday.AddDate(0, 0, 3), false, false),   // exactly the horizon: in
		nudgeBill("Gym", nudgeToday.AddDate(0, 0, 4), false, false),    // one day past: out
		nudgeBill("Phone", nudgeToday.AddDate(0, 0, -2), false, true),  // overdue: in
		nudgeBill("Netflix", nudgeToday.AddDate(0, 0, 1), true, false), // autopay: out
		{Bill: domain.Bill{Name: "Settled"}},                           // no next due: out
	}}}
	budget := &budgetStub{view: usecase.BudgetMonthView{Budget: &domain.Budget{}, Categories: []usecase.BudgetCategoryView{
		{CategoryName: "Groceries", Cap: domain.Money{Amount: 50000}, Spent: domain.Money{Amount: 40000}}, // exactly 80%: in
		{CategoryName: "Fun", Cap: domain.Money{Amount: 50000}, Spent: domain.Money{Amount: 39999}},       // just under: out
		{CategoryName: "Uncapped", Cap: domain.Money{Amount: 0}, Spent: domain.Money{Amount: 99}},         // no cap: out
	}}}
	svc := usecase.NewNudgeService(usecase.NudgeDeps{Bills: bills, Budgets: budget})

	text, ok, err := svc.Compose(context.Background(), "h-1", "SGD", nudgeToday)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	for _, want := range []string{"Rent SGD 123.45, Fri 11 Sep", "Phone", "overdue since", "Groceries 80% used (400.00 of 500.00)"} {
		if !strings.Contains(text, want) {
			t.Errorf("digest lacks %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"Gym", "Netflix", "Settled", "Fun", "Uncapped"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("digest must not name %q:\n%s", forbidden, text)
		}
	}
}

func TestComposeSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	svc := usecase.NewNudgeService(usecase.NudgeDeps{
		Bills:   billsStub{view: usecase.BillsView{Bills: []usecase.BillView{nudgeBill("Gym", nudgeToday.AddDate(0, 0, 10), false, false)}}},
		Budgets: &budgetStub{view: usecase.BudgetMonthView{}}, // no budget set this month
	})
	text, ok, err := svc.Compose(context.Background(), "h-1", "SGD", nudgeToday)
	if err != nil || ok || text != "" {
		t.Fatalf("got %q ok=%v err=%v, want nothing", text, ok, err)
	}
}

func TestRunOnceClaimsBeforeSendingAndNeverSendsTwiceInADay(t *testing.T) {
	repo := &nudgeRepoStub{recipients: []usecase.NudgeRecipient{{ChatID: 1, HouseholdID: "h-1", Currency: "SGD"}}}
	sender := &nudgeSenderSpy{}
	svc := usecase.NewNudgeService(usecase.NudgeDeps{
		Recipients: repo, Sender: sender,
		Bills:   billsStub{view: usecase.BillsView{Bills: []usecase.BillView{nudgeBill("Rent", nudgeToday, false, false)}}},
		Budgets: &budgetStub{},
	})
	svc.RunOnce(context.Background(), nudgeToday)
	svc.RunOnce(context.Background(), nudgeToday.Add(15*time.Minute))
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d messages across two ticks, want 1", len(sender.sent))
	}
	svc.RunOnce(context.Background(), nudgeToday.AddDate(0, 0, 1))
	if len(sender.sent) != 2 {
		t.Fatalf("the next day must send again, got %d", len(sender.sent))
	}
}

func TestRunOnceReleasesTheClaimWhenTheSendFailsAndGoesOnToTheNextRecipient(t *testing.T) {
	repo := &nudgeRepoStub{recipients: []usecase.NudgeRecipient{
		{ChatID: 1, HouseholdID: "h-1", Currency: "SGD"},
		{ChatID: 2, HouseholdID: "h-2", Currency: "SGD"},
	}}
	sender := &nudgeSenderSpy{fail: true}
	svc := usecase.NewNudgeService(usecase.NudgeDeps{
		Recipients: repo, Sender: sender,
		Bills:   billsStub{view: usecase.BillsView{Bills: []usecase.BillView{nudgeBill("Rent", nudgeToday, false, false)}}},
		Budgets: &budgetStub{},
	})
	svc.RunOnce(context.Background(), nudgeToday)
	if len(repo.released) != 2 {
		t.Fatalf("both failed sends must release their claims, released %v", repo.released)
	}
	sender.fail = false
	svc.RunOnce(context.Background(), nudgeToday.Add(15*time.Minute))
	if len(sender.sent) != 2 {
		t.Fatalf("after release the next tick must deliver both, sent %d", len(sender.sent))
	}
}

func TestRunOnceKeepsTheClaimOnAQuietDay(t *testing.T) {
	repo := &nudgeRepoStub{recipients: []usecase.NudgeRecipient{{ChatID: 1, HouseholdID: "h-1", Currency: "SGD"}}}
	sender := &nudgeSenderSpy{}
	svc := usecase.NewNudgeService(usecase.NudgeDeps{Recipients: repo, Sender: sender, Bills: billsStub{}, Budgets: &budgetStub{}})
	svc.RunOnce(context.Background(), nudgeToday)
	if len(sender.sent) != 0 || len(repo.claims) != 1 || len(repo.released) != 0 {
		t.Fatalf("quiet day: sent=%d claims=%d released=%d; want 0, 1, 0", len(sender.sent), len(repo.claims), len(repo.released))
	}
}

func TestNudgeDueIsTheLocalClockNotUTC(t *testing.T) {
	sgt := time.FixedZone("SGT", 8*3600)
	cases := []struct {
		name string
		now  time.Time
		due  bool
		day  string
	}{
		{"08:59 SGT is before", time.Date(2026, 9, 8, 0, 59, 0, 0, time.UTC), false, "2026-09-08"},
		{"09:00 SGT exactly", time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC), true, "2026-09-08"},
		{"23:50 SGT still nudgeToday", time.Date(2026, 9, 8, 15, 50, 0, 0, time.UTC), true, "2026-09-08"},
		{"00:10 SGT is tomorrow, before", time.Date(2026, 9, 8, 16, 10, 0, 0, time.UTC), false, "2026-09-09"},
	}
	for _, c := range cases {
		day, due := usecase.NudgeDue(c.now, "09:00", sgt)
		if due != c.due || day.Format("2006-01-02") != c.day {
			t.Errorf("%s: due=%v day=%s, want %v %s", c.name, due, day.Format("2006-01-02"), c.due, c.day)
		}
	}
	if _, due := usecase.NudgeDue(time.Now(), "nine", sgt); due {
		t.Fatal("an unparseable clock must never be due")
	}
}

// A tick at 00:15 SGT on 1 September is 31 August in UTC. The digest must
// read September's budget and a three-day horizon from the 1st, the way the
// screens do (UTC-midnight dates), not from the zoned instant.
func TestRunOnceReadsTheLocalCalendarDayAsTheProductDoes(t *testing.T) {
	sgt := time.FixedZone("SGT", 8*3600)
	tick := time.Date(2026, 9, 1, 0, 15, 0, 0, sgt)
	repo := &nudgeRepoStub{recipients: []usecase.NudgeRecipient{{ChatID: 1, HouseholdID: "h-1", Currency: "SGD"}}}
	budget := &budgetStub{}
	svc := usecase.NewNudgeService(usecase.NudgeDeps{Recipients: repo, Sender: &nudgeSenderSpy{}, Bills: billsStub{}, Budgets: budget})
	svc.RunOnce(context.Background(), tick)
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if len(budget.asked) != 1 || !budget.asked[0].Equal(want) {
		t.Fatalf("budget month asked = %v, want %v", budget.asked, want)
	}
	if _, claimed := repo.claims[key(1, "h-1", want)]; !claimed {
		t.Fatalf("claim must be for the local date 2026-09-01, claims=%v", repo.claims)
	}
}
