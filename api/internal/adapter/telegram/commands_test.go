package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func msg(id int64, chat int64, text string) Update {
	u := Update{UpdateID: id, Message: &Message{Text: text}}
	u.Message.Chat.ID = chat
	return u
}

func TestParseCommandGrammar(t *testing.T) {
	cases := []struct {
		text string
		want Command
		ok   bool
	}{
		{"/spend 84.50 groceries", Command{Name: "spend", Amount: "84.50", Description: "groceries"}, true},
		{"/spend 12 coffee and cake #\"Dining out\" @\"DBS Savings\"", Command{Name: "spend", Amount: "12", Description: "coffee and cake", Category: "Dining out", Account: "DBS Savings"}, true},
		{"/income 6500 salary @OCBC", Command{Name: "income", Amount: "6500", Description: "salary", Account: "OCBC"}, true},
		{"/SPEND@HearthOinkBot 5 gum", Command{Name: "spend", Amount: "5", Description: "gum"}, true},
		{"/spend", Command{Name: "spend"}, true},
		{"/balance", Command{Name: "balance"}, true},
		{"/recent extra words", Command{Name: "recent"}, true},
		{"/help", Command{Name: "help"}, true},
		{"/start abc", Command{}, false},
		{"just words", Command{}, false},
		{"/unknown 5", Command{}, false},
		{"", Command{}, false},
	}
	for _, c := range cases {
		got, ok := ParseCommand(msg(7, 42, c.text))
		if ok != c.ok {
			t.Errorf("%q: ok=%v want %v", c.text, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		c.want.ChatID, c.want.UpdateID = 42, 7
		if got != c.want {
			t.Errorf("%q:\n got %+v\nwant %+v", c.text, got, c.want)
		}
	}
	if _, ok := ParseCommand(Update{UpdateID: 1}); ok {
		t.Fatal("an update with no message must not parse")
	}
}

// --- Commander doubles

type resolverStub struct {
	member domain.Membership
	err    error
}

func (r resolverStub) Resolve(context.Context, int64) (domain.Membership, error) {
	return r.member, r.err
}

type serviceSpy struct {
	spends []usecase.TelegramSpend
	result usecase.TelegramSpendResult
	err    error
}

func (s *serviceSpy) LogSpend(_ context.Context, in usecase.TelegramSpend) (usecase.TelegramSpendResult, error) {
	s.spends = append(s.spends, in)
	return s.result, s.err
}
func (s *serviceSpy) Balances(context.Context, string) ([]usecase.AccountView, error) {
	return []usecase.AccountView{{Account: domain.Account{Nickname: "DBS"}, Balance: domain.Money{Amount: 123456, Currency: "SGD"}}}, nil
}
func (s *serviceSpy) Recent(context.Context, string, int) ([]usecase.TransactionView, error) {
	return nil, nil
}

type senderSpy struct {
	mu   sync.Mutex
	sent []string
}

func (s *senderSpy) SendMessage(_ context.Context, _ int64, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, text)
	return nil
}

func owner() domain.Membership {
	return domain.Membership{ID: "m-1", HouseholdID: "h-1", Role: domain.RoleOwner, Capabilities: domain.Capabilities{domain.CapMoney}}
}

func spendCmd() Command {
	return Command{ChatID: 42, UpdateID: 9, Name: "spend", Amount: "84.50", Description: "groceries", Category: "Groceries"}
}

func TestAnUnlinkedChatIsRefusedBeforeAnyServiceCall(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	c := NewCommander(resolverStub{err: domain.ErrNotFound}, svc, sender)
	c.HandleCommand(context.Background(), spendCmd())
	if len(svc.spends) != 0 || len(sender.sent) != 1 || sender.sent[0] != replyNotLinked {
		t.Fatalf("spends=%d sent=%v", len(svc.spends), sender.sent)
	}
}

func TestALimitedMemberAndAnOwnerWithoutMoneyAreRefused(t *testing.T) {
	limited := domain.Membership{ID: "m-2", HouseholdID: "h-1", Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapMoney}}
	ownerNoMoney := domain.Membership{ID: "m-3", HouseholdID: "h-1", Role: domain.RoleOwner, Capabilities: domain.Capabilities{domain.CapCalendar}}
	for name, m := range map[string]domain.Membership{"limited with money": limited, "owner without money": ownerNoMoney} {
		svc, sender := &serviceSpy{}, &senderSpy{}
		NewCommander(resolverStub{member: m}, svc, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "balance"})
		if len(sender.sent) != 1 || sender.sent[0] != replyNotOwner {
			t.Errorf("%s: sent %v", name, sender.sent)
		}
	}
}

func TestAnOwnerSpendReachesTheServiceWithTheUpdateIDAndGetsAReceipt(t *testing.T) {
	svc := &serviceSpy{result: usecase.TelegramSpendResult{
		Transaction: domain.Transaction{Description: "groceries", Amount: domain.Money{Amount: 8450, Currency: "SGD"}},
		AccountName: "DBS Savings", MinorUnits: 2,
	}}
	sender := &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
	if len(svc.spends) != 1 {
		t.Fatalf("service called %d times", len(svc.spends))
	}
	in := svc.spends[0]
	if in.HouseholdID != "h-1" || in.MembershipID != "m-1" || in.UpdateID != 9 || in.Kind != domain.TransactionExpense || in.AmountText != "84.50" || in.CategoryName != "Groceries" {
		t.Fatalf("service input %+v", in)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "Logged -84.50 SGD — groceries (DBS Savings)" {
		t.Fatalf("receipt %v", sender.sent)
	}
}

func TestAReplayedSpendSaysAlreadyLogged(t *testing.T) {
	svc := &serviceSpy{result: usecase.TelegramSpendResult{
		Transaction: domain.Transaction{Description: "x", Amount: domain.Money{Amount: 100, Currency: "SGD"}}, AccountName: "A", MinorUnits: 2, Replayed: true,
	}}
	sender := &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
	if !strings.HasPrefix(sender.sent[0], "Already logged") {
		t.Fatalf("got %q", sender.sent[0])
	}
}

func TestRefusalsAreExplainedAndInternalErrorsAreNot(t *testing.T) {
	cases := map[error]string{
		&usecase.ResolutionError{What: "account", Typed: "DBX", Candidates: []string{"DBS", "OCBC"}}:    `No account called "DBX". One of: DBS, OCBC`,
		&usecase.ResolutionError{What: "account", Candidates: []string{"DBS", "OCBC"}, Ambiguous: true}: "Which account? Add @name — one of: DBS, OCBC",
		domain.ErrInvalidMoney:  "That amount could not be read",
		errors.New("pgx: boom"): "That could not be saved",
	}
	for err, want := range cases {
		svc, sender := &serviceSpy{err: err}, &senderSpy{}
		NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), spendCmd())
		if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], want) {
			t.Errorf("%v: sent %v, want containing %q", err, sender.sent, want)
		}
		if strings.Contains(sender.sent[0], "pgx") {
			t.Errorf("internal error text leaked to chat: %q", sender.sent[0])
		}
	}
}

func TestSpendWithoutAmountOrDescriptionGetsUsageNotAWrite(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	c := NewCommander(resolverStub{member: owner()}, svc, sender)
	c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "spend", Amount: "5"})
	c.HandleCommand(context.Background(), Command{ChatID: 1, Name: "income"})
	if len(svc.spends) != 0 || len(sender.sent) != 2 || sender.sent[0] != replyUsageSpend || sender.sent[1] != replyUsageIncome {
		t.Fatalf("spends=%d sent=%v", len(svc.spends), sender.sent)
	}
}

func TestBalanceFormatsEachAccountInItsCurrency(t *testing.T) {
	svc, sender := &serviceSpy{}, &senderSpy{}
	NewCommander(resolverStub{member: owner()}, svc, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "balance"})
	if sender.sent[0] != "DBS: SGD 1234.56" {
		t.Fatalf("got %q", sender.sent[0])
	}
}

func TestHelpNeedsNoLinkedAccount(t *testing.T) {
	sender := &senderSpy{}
	NewCommander(resolverStub{err: domain.ErrNotFound}, &serviceSpy{}, sender).HandleCommand(context.Background(), Command{ChatID: 1, Name: "help"})
	if sender.sent[0] != replyHelp {
		t.Fatalf("got %q", sender.sent[0])
	}
}
