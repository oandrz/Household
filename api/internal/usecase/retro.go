package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// MoodPoint is one point on the twelve-month mood chart. HasMood false is a
// gap -- a month with no finished retro, or a finished retro that carries no
// mood -- and is never rendered as Mood == 0, because 0 is not a mood
// (spec's formulas table, "Mood over 12 months": "Never zero -- zero is a
// claim, the same rule Budget applies to transactions it cannot convert").
type MoodPoint struct {
	Month   time.Time
	Mood    int
	HasMood bool
}

// RetrosView is the whole Retros history screen: every row the list
// displays, the mood chart, the finished count and its "since" month, and
// which month (if any) the Start-retro button offers.
type RetrosView struct {
	Summaries  []RetroSummary
	Mood       []MoodPoint
	DoneCount  int
	Since      *time.Time
	StartMonth *time.Time
}

// RetroView is one month's detail screen: the retro itself, its own
// actions, and the carry-over offer from the immediately previous month.
type RetroView struct {
	Retro     RetroRecord
	Actions   []RetroActionRecord
	CarryOver []RetroActionRecord
}

// RetroService composes the Retros screen and every write against it. Like
// every other service here it takes no actor parameter: services enforce
// what is *valid*, middleware enforces who is *asking* -- the marriage
// capability and the owner check live in the router (Task 8).
type RetroService struct {
	retros  RetroRepository
	actions RetroActionRepository
}

func NewRetroService(retros RetroRepository, actions RetroActionRepository) *RetroService {
	return &RetroService{retros: retros, actions: actions}
}

// monthKey turns a month into a value safe to use as a map key across two
// time.Time values that name the same calendar month but disagree on
// *time.Location or time-of-day -- which two round trips through a
// database column are free to do even when both ultimately mean "UTC"
// (different *time.Location pointers compare unequal, and time.Time is a
// map key by struct equality, location pointer included). Year()/Month()
// read the calendar fields as the value's own location sees them, which is
// exactly what "the month this row belongs to" means here -- there is no
// second, competing interpretation to convert between.
func monthKey(t time.Time) int { return t.Year()*12 + int(t.Month()) }

// List composes the whole history screen for one household: every summary
// row (newest first, as RetroRepository.List's own contract already
// guarantees -- this method does not re-sort), the twelve-month mood chart,
// the finished count and its earliest month, and the startable month. today
// drives the chart's window and the startable-month calculation, taken as a
// parameter -- never read from a clock in here -- so every figure is
// deterministic in tests and the wall clock is read exactly once, at the
// HTTP layer.
//
// Every derived figure below cites the spec's formulas table
// (docs/superpowers/specs/2026-08-16-hearth-retros-design.md) so a later
// reader does not have to reverse-engineer the rule from the arithmetic.
func (s *RetroService) List(ctx context.Context, householdID string, today time.Time) (RetrosView, error) {
	records, err := s.retros.List(ctx, householdID)
	if err != nil {
		return RetrosView{}, err
	}

	current := startOfMonth(today)
	previous := current.AddDate(0, -1, 0)

	summaries := make([]RetroSummary, 0, len(records))
	finishedMood := make(map[int]*int, len(records)) // monthKey -> mood, finished retros only
	var doneCount int
	var since *time.Time
	var currentExists, previousExists bool

	for _, rec := range records {
		summary := rec
		// "History row": the quoted line is Notes' first sentence, derived
		// here rather than stored -- RetroSummary's own doc comment.
		summary.Quote = domain.FirstSentence(rec.Retro.Notes)
		summaries = append(summaries, summary)

		// Normalise before any comparison -- budget.go's startOfMonth is the
		// house convention (budget.go:638's own comment; BudgetService.Month
		// applies it before comparing for the identical reason). A repository
		// value that is not exactly midnight-on-the-first would otherwise
		// silently miss every Equal/Before check below, on both List's
		// finished-month bookkeeping and the mood chart's map key.
		month := startOfMonth(rec.Retro.Month)
		if month.Equal(current) {
			currentExists = true
		}
		if month.Equal(previous) {
			previousExists = true
		}

		// "12 done since Aug 2025": count(*) WHERE completed_at IS NOT NULL,
		// "since" is min(month) of those rows. A draft (CompletedAt nil)
		// counts toward neither -- decision 2, "a draft is not a data point".
		finished := rec.Retro.CompletedAt != nil
		if finished {
			doneCount++
			if since == nil || month.Before(*since) {
				m := month
				since = &m
			}
		}

		// "Mood over 12 months": a finished retro's own mood. `finished` is
		// tested here on its own -- deliberately not folded into the
		// doneCount branch above -- so a draft's mood cannot reach the chart
		// even if the doneCount/since bookkeeping were somehow untouched
		// (decision 2's second half). A finished retro with no mood picked
		// (Mood nil) leaves no entry, which the loop below already reads as
		// a gap.
		if finished && rec.Retro.Mood != nil {
			finishedMood[monthKey(month)] = rec.Retro.Mood
		}
	}

	mood := make([]MoodPoint, 12)
	for i := range mood {
		m := current.AddDate(0, -(11 - i), 0)
		point := MoodPoint{Month: m}
		if moodVal, ok := finishedMood[monthKey(m)]; ok {
			point.Mood = *moodVal
			point.HasMood = true
		}
		mood[i] = point
	}

	// "Startable month": the earlier of {previous month, current month}
	// with no retro row; nil when both already have one (decision 5).
	var startMonth *time.Time
	if sm, ok := domain.StartableMonth(today, currentExists, previousExists); ok {
		startMonth = &sm
	}

	return RetrosView{
		Summaries:  summaries,
		Mood:       mood,
		DoneCount:  doneCount,
		Since:      since,
		StartMonth: startMonth,
	}, nil
}

// Month composes one month's detail screen: the retro, its own actions, and
// the "Still open from July" carry-over offer -- the immediately previous
// month's unticked actions only, never further back (spec decision 4).
// domain.ErrNotFound from ByMonth is returned untouched: the page reads a
// missing retro as "not started," not as an error, and this method does not
// obscure that by wrapping it.
func (s *RetroService) Month(ctx context.Context, householdID string, month time.Time) (RetroView, error) {
	// Normalise once, at the top, and use the normalised value for every
	// call below -- both ByMonth's lookup and the carry-over month it feeds
	// into. Normalising only for the carry-over computation (as an earlier
	// version of this method did) while passing the raw, possibly
	// mid-month `month` straight to ByMonth would look up the right retro
	// by luck whenever a caller already normalises, and the wrong one
	// (domain.ErrNotFound) the moment one does not.
	month = startOfMonth(month)

	retro, err := s.retros.ByMonth(ctx, householdID, month)
	if err != nil {
		return RetroView{}, err
	}

	actions, err := s.actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		return RetroView{}, err
	}

	previous := month.AddDate(0, -1, 0)
	carryOver, err := s.actions.OpenInMonth(ctx, householdID, previous)
	if err != nil {
		return RetroView{}, err
	}

	return RetroView{Retro: retro, Actions: actions, CarryOver: carryOver}, nil
}

// Start creates a new draft for the month domain.StartableMonth chooses from
// the household's own retros -- the earlier of {previous month, current
// month} that has none yet. It never falls back to "today's month anyway"
// when neither candidate is free: a stale tab left open across a month
// boundary would otherwise be able to file a retro against a month the
// button never actually offered it. Both candidates already having a retro
// is domain.ErrRetroNothingToStart, not a silently invented third month.
func (s *RetroService) Start(ctx context.Context, householdID string, today time.Time) (RetroRecord, error) {
	current := startOfMonth(today)
	previous := current.AddDate(0, -1, 0)

	currentExists, err := s.retroExists(ctx, householdID, current)
	if err != nil {
		return RetroRecord{}, err
	}
	previousExists, err := s.retroExists(ctx, householdID, previous)
	if err != nil {
		return RetroRecord{}, err
	}

	month, ok := domain.StartableMonth(today, currentExists, previousExists)
	if !ok {
		return RetroRecord{}, domain.ErrRetroNothingToStart
	}
	return s.retros.Create(ctx, householdID, month)
}

// retroExists answers whether householdID already has a retro for month by
// asking ByMonth and translating its domain.ErrNotFound into false: "no
// retro yet" is the expected half of this question, not a failure to
// propagate. Any other error is returned untouched -- a real infrastructure
// failure must not be read as "this month is free."
func (s *RetroService) retroExists(ctx context.Context, householdID string, month time.Time) (bool, error) {
	_, err := s.retros.ByMonth(ctx, householdID, month)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, domain.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// Save validates the mood BEFORE the repository is ever called -- an
// impossible mood must produce zero writes, not a write the repository then
// has to refuse (TestRetroSaveRefusesAnImpossibleMood reads the double's
// write count to prove exactly that ordering, not just that an error came
// back). Mood nil clears the mood, which a household can legitimately do,
// so it is only checked when a value is actually present.
//
// u.Month is normalised with startOfMonth here, not left to the caller.
// RetroUpdate.Month's own doc comment says the repository never normalises
// and "the caller normalises" -- RetroService IS that caller, the same way
// List and Month (Task 3) normalise before comparing against or looking up
// a stored value. Skipping this would be the worst-shaped failure available
// here: RetroRepository.Update matches a row on id + household + month
// together, so an un-normalised month would silently miss the match and
// come back domain.ErrNotFound -- telling an editor their retro vanished
// when in fact it is sitting one PATCH away, correctly versioned, under the
// midnight-UTC month a caller merely forgot to round to.
//
// The three text fields are trimmed; Version passes straight through
// unmodified. The version comparison itself is deliberately NOT done here:
// RetroRepository.Update's own guarded UPDATE is the only place that can
// compare against the stored value atomically, and re-checking it in this
// layer first would open exactly the read-then-write race that guard exists
// to close.
func (s *RetroService) Save(ctx context.Context, u RetroUpdate) (RetroRecord, error) {
	if u.Mood != nil {
		if _, err := domain.ParseMood(*u.Mood); err != nil {
			return RetroRecord{}, err
		}
	}

	u.Month = startOfMonth(u.Month)
	u.WentWell = strings.TrimSpace(u.WentWell)
	u.WasHard = strings.TrimSpace(u.WasHard)
	u.Notes = strings.TrimSpace(u.Notes)

	return s.retros.Update(ctx, u)
}

// Finish stamps the retro complete. RetroRepository.Complete is itself
// idempotent -- finishing an already-finished retro keeps the FIRST
// timestamp rather than moving it forward -- so a double-submit or a retry
// after a dropped response is harmless and needs no guard here.
func (s *RetroService) Finish(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error) {
	return s.retros.Complete(ctx, householdID, retroID, at)
}

// DiscardDraft removes a retro that has not been finished. The refusal for a
// finished retro lives in RetroRepository.DeleteDraft's own WHERE ...
// completed_at IS NULL -- domain.ErrNotFound passes through untouched here,
// never re-checked with a service-level `if`, so there is exactly one place
// that decides whether a retro is still a draft (the same reasoning
// DeleteDraft's own doc comment gives for putting the condition in SQL
// rather than in a check-then-delete).
func (s *RetroService) DiscardDraft(ctx context.Context, householdID, retroID string) error {
	return s.retros.DeleteDraft(ctx, householdID, retroID)
}

// AddAction refuses a blank body with domain.ErrRetroActionBodyRequired --
// the design's own control is "+ Add an action & assign it to one of you",
// and a blank row on the retro detail would be indistinguishable from a
// rendering bug -- and stores the trimmed body.
func (s *RetroService) AddAction(ctx context.Context, in RetroActionInput) (RetroActionRecord, error) {
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" {
		return RetroActionRecord{}, domain.ErrRetroActionBodyRequired
	}
	return s.actions.Add(ctx, in)
}

// SetActionDone ticks or unticks one action. It touches only
// RetroActionRepository, never RetroRepository: an action's own done state
// must not bump the retro's version, or one partner ticking every action
// this month would invalidate the other's already-open editor tab for no
// reason connected to what they are editing.
func (s *RetroService) SetActionDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error {
	return s.actions.SetDone(ctx, householdID, actionID, done, at)
}

// RemoveAction deletes one action. RetroActionRepository.Remove's own
// domain.ErrNotFound on a zero-row match passes through untouched.
func (s *RetroService) RemoveAction(ctx context.Context, householdID, actionID string) error {
	return s.actions.Remove(ctx, householdID, actionID)
}
