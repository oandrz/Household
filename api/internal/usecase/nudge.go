package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The bot speaks first. Once a day, each NudgeRecipient gets one message per
// household: bills overdue or due within three days, and budget lines at or
// past 80%. Rules, not a model, so it's deterministic, free, and needs no
// language model. Nothing to say means no message -- never "all fine".
const (
	// nudgeBillHorizon is how far ahead a bill counts as "due soon" here.
	// Narrower than BillView.DueSoon's 30 days on purpose: that heading
	// organises a screen, this interrupts a phone.
	nudgeBillHorizon = 3
	// nudgeBudgetPercent is the line a category must reach to be named.
	nudgeBudgetPercent = 80
)

// BillsReader and BudgetReader are the two reads the digest makes, declared
// here so the service is testable against doubles; BillService and
// BudgetService satisfy them.
type BillsReader interface {
	List(ctx context.Context, householdID string, includeArchived bool, today time.Time) (BillsView, error)
}

type BudgetReader interface {
	Month(ctx context.Context, householdID string, month, today time.Time) (BudgetMonthView, error)
}

type NudgeDeps struct {
	Recipients NudgeRepository
	Bills      BillsReader
	Budgets    BudgetReader
	Sender     TelegramSender
}

type NudgeService struct{ d NudgeDeps }

func NewNudgeService(d NudgeDeps) *NudgeService { return &NudgeService{d: d} }

// Compose builds one household's digest for today. ok is false when there is
// nothing worth a message.
func (s *NudgeService) Compose(ctx context.Context, householdID, currency string, today time.Time) (string, bool, error) {
	units := domain.MinorUnitsFor(currency)
	var lines []string

	bills, err := s.d.Bills.List(ctx, householdID, false, today)
	if err != nil {
		return "", false, fmt.Errorf("nudge bills: %w", err)
	}
	horizon := today.AddDate(0, 0, nudgeBillHorizon)
	for _, b := range bills.Bills {
		// Autopay pays itself; reminding someone about it is the noise that
		// gets the whole digest muted on day two.
		if b.Bill.NextDue == nil || b.Bill.Autopay || b.Bill.NextDue.After(horizon) {
			continue
		}
		amount := domain.FormatAmount(b.Bill.Amount.Amount, domain.MinorUnitsFor(b.Bill.Amount.Currency))
		when := b.Bill.NextDue.Format("Mon 2 Jan")
		if b.Overdue {
			when = "overdue since " + when
		}
		lines = append(lines, fmt.Sprintf("• %s %s %s, %s", b.Bill.Name, b.Bill.Amount.Currency, amount, when))
	}
	var billText string
	if len(lines) > 0 {
		billText = "Bills due soon:\n" + strings.Join(lines, "\n")
	}

	lines = lines[:0]
	month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	budget, err := s.d.Budgets.Month(ctx, householdID, month, today)
	if err != nil {
		return "", false, fmt.Errorf("nudge budget: %w", err)
	}
	if budget.Budget != nil {
		for _, c := range budget.Categories {
			// Integer arithmetic only: spent*100 >= cap*80 is the 80% line
			// without a float anywhere near money.
			if c.Cap.Amount <= 0 || c.Spent.Amount*100 < c.Cap.Amount*nudgeBudgetPercent {
				continue
			}
			pct := c.Spent.Amount * 100 / c.Cap.Amount
			lines = append(lines, fmt.Sprintf("• %s %d%% used (%s of %s)", c.CategoryName, pct,
				domain.FormatAmount(c.Spent.Amount, units), domain.FormatAmount(c.Cap.Amount, units)))
		}
	}
	var budgetText string
	if len(lines) > 0 {
		budgetText = "Budget running hot:\n" + strings.Join(lines, "\n")
	}

	switch {
	case billText != "" && budgetText != "":
		return billText + "\n\n" + budgetText, true, nil
	case billText != "":
		return billText, true, nil
	case budgetText != "":
		return budgetText, true, nil
	}
	return "", false, nil
}

// RunOnce delivers today's digest to every recipient who hasn't had it.
// Claim comes first, like the transaction idempotency key, so two ticks or
// a restart can't send twice. A failure is logged and the loop continues;
// a failed send releases its claim so the next tick retries. now is the
// tick's local time; its calendar date is the day being claimed.
func (s *NudgeService) RunOnce(ctx context.Context, now time.Time) {
	// The local calendar date, as UTC midnight -- the shape every bill and
	// budget comparison uses. Passing the zoned instant through instead
	// would make a UTC+8 tick before 08:00 local read yesterday's date: a
	// two-day horizon, and last month's budget on the 1st.
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	recipients, err := s.d.Recipients.Recipients(ctx)
	if err != nil {
		slog.Error("nudge recipients failed", "error", err)
		return
	}
	for _, r := range recipients {
		claimed, err := s.d.Recipients.Claim(ctx, r.ChatID, r.HouseholdID, day)
		if err != nil {
			slog.Error("nudge claim failed", "error", err, "household", r.HouseholdID)
			continue
		}
		if !claimed {
			continue
		}
		text, ok, err := s.Compose(ctx, r.HouseholdID, r.Currency, day)
		if err != nil {
			// The claim stands: a household whose data cannot be read will
			// not be read again every fifteen minutes, and the log says why.
			slog.Error("nudge compose failed", "error", err, "household", r.HouseholdID)
			continue
		}
		if !ok {
			continue // a quiet day is still done
		}
		if err := s.d.Sender.SendMessage(ctx, r.ChatID, text); err != nil {
			slog.Error("nudge send failed", "error", err, "household", r.HouseholdID)
			if err := s.d.Recipients.Release(ctx, r.ChatID, r.HouseholdID, day); err != nil {
				slog.Error("nudge release failed", "error", err, "household", r.HouseholdID)
			}
			continue
		}
		slog.Info("nudge sent", "household", r.HouseholdID)
	}
}

// NudgeDue says whether a tick at now should deliver: local time in loc is at
// or past at (an "HH:MM" clock), and the local date is the day to claim. A
// tick every fifteen minutes plus this rule means a restart at 09:01 still
// delivers at 09:15, and the claim makes every later tick free.
func NudgeDue(now time.Time, at string, loc *time.Location) (day time.Time, due bool) {
	local := now.In(loc)
	var hh, mm int
	if _, err := fmt.Sscanf(at, "%d:%d", &hh, &mm); err != nil {
		return local, false
	}
	start := time.Date(local.Year(), local.Month(), local.Day(), hh, mm, 0, 0, loc)
	return local, !local.Before(start)
}
