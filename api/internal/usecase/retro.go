package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// MoodPoint is one point on the twelve-month mood chart. HasMood false is a
// gap -- no finished retro that month, or one with no mood recorded -- and
// is never rendered as Mood == 0: zero would read as a claim about the
// mood, not as "no data" (spec's formulas table, "Never zero -- zero is a
// claim").
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
// what is *valid*, the channel's inbound edge enforces who is *asking*
// (ADR 8) -- the marriage capability and the owner check live in the router.
type RetroService struct {
	retros  RetroRepository
	actions RetroActionRepository
}

func NewRetroService(retros RetroRepository, actions RetroActionRepository) *RetroService {
	return &RetroService{retros: retros, actions: actions}
}

// monthKey turns a month into a value safe to use as a map key. Two
// time.Time values naming the same calendar month can still disagree on
// *time.Location or time-of-day -- a round trip through a database column
// is free to do that even when both ultimately mean "UTC" -- and time.Time
// is a map key by struct equality, location pointer included, so comparing
// the values directly would treat "the same month" as two different keys.
// Year() and Month() read the calendar fields in the value's own location,
// which is exactly the month the row belongs to, so there is nothing to
// convert.
func monthKey(t time.Time) int { return t.Year()*12 + int(t.Month()) }

// List composes the whole history screen for one household: every summary
// row (newest first, per RetroRepository.List's own contract), the
// twelve-month mood chart, the finished count and its earliest month, and
// the startable month. today is a parameter, never read from a clock here
// -- the HTTP layer reads the wall clock once -- so every figure is
// deterministic in tests. Each derived figure below follows the spec's
// formulas table (docs/superpowers/specs/2026-08-16-hearth-retros-design.md).
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

		// Normalise before any comparison -- startOfMonth is the house
		// convention (BudgetService.Month does the same). An un-normalised
		// value would silently miss every Equal/Before check below, in both
		// the finished-month bookkeeping and the mood chart's map key.
		month := startOfMonth(rec.Retro.Month)
		if month.Equal(current) {
			currentExists = true
		}
		if month.Equal(previous) {
			previousExists = true
		}

		// "12 done since Aug 2025": count(*) WHERE completed_at IS NOT NULL,
		// "since" is min(month) of those rows. A draft (CompletedAt nil)
		// counts toward neither -- "a draft is not a data point".
		finished := rec.Retro.CompletedAt != nil
		if finished {
			doneCount++
			if since == nil || month.Before(*since) {
				m := month
				since = &m
			}
		}

		// "Mood over 12 months": a finished retro's own mood, read
		// independently of the doneCount branch above so a draft's mood can
		// never reach the chart (same rule: a draft is not a data point).
		// Mood nil leaves no entry, which the loop below reads as a gap.
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
	// with no retro row; nil when both already have one.
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
// month's unticked actions only, never further back. domain.ErrNotFound from
// ByMonth is returned untouched: the page reads a missing retro as "not
// started," not as an error, and this method does not obscure that by
// wrapping it.
func (s *RetroService) Month(ctx context.Context, householdID string, month time.Time) (RetroView, error) {
	// Normalise once, at the top, and reuse the value for both ByMonth's
	// lookup and the carry-over month -- don't normalise only for the
	// carry-over and pass the raw `month` to ByMonth, which finds the right
	// retro by luck and domain.ErrNotFound the moment a caller doesn't
	// already normalise.
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

// Start creates a new draft for the month domain.StartableMonth picks -- the
// earlier of {previous, current} that has none yet. It never falls back to
// "today's month anyway": a stale tab open across a month boundary could
// otherwise file a retro against a month the button never offered. Both
// already having a retro is domain.ErrRetroNothingToStart, not an invented
// third month.
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

// retroExists asks ByMonth whether householdID has a retro for month,
// translating domain.ErrNotFound into false -- "no retro yet" is expected,
// not a failure. Any other error passes through untouched: an
// infrastructure failure must not be read as "this month is free."
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

// Save validates the mood before the repository is ever called: an
// impossible mood must produce zero writes, not one the repository has to
// refuse (TestRetroSaveRefusesAnImpossibleMood checks the write count, not
// just the error). Mood nil is a legitimate clear, so it's checked only
// when a value is present.
//
// u.Month is normalised here since RetroUpdate.Month's doc comment says the
// repository never does. Skipping it would break Update's zero-row recheck:
// the guarded UPDATE matches on household + id + version, never month, so
// an un-normalised month would not stop the write itself from landing --
// but ByMonth's recheck (which matches on household + month, to tell "gone"
// apart from "version moved") would miss the very row whose version
// failed, misreporting a live conflict (domain.ErrRetroChanged) as
// domain.ErrNotFound.
//
// The three text fields are trimmed; Version passes through unmodified. The
// version comparison itself is deliberately not done here: only
// RetroRepository.Update's guarded UPDATE can compare atomically, and
// re-checking first would reopen the read-then-write race that guard
// closes.
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

// Finish stamps the retro complete. RetroRepository.Complete is idempotent
// -- it keeps the first timestamp rather than moving it forward -- so a
// double-submit or retry needs no guard here.
func (s *RetroService) Finish(ctx context.Context, householdID, retroID string, at time.Time) (RetroRecord, error) {
	return s.retros.Complete(ctx, householdID, retroID, at)
}

// DiscardDraft removes a retro that has not been finished; the refusal for a
// finished one lives in DeleteDraft's own WHERE completed_at IS NULL clause.
// domain.ErrNotFound passes through untouched and is never re-checked here,
// so exactly one place decides whether a retro is still a draft.
func (s *RetroService) DiscardDraft(ctx context.Context, householdID, retroID string) error {
	return s.retros.DeleteDraft(ctx, householdID, retroID)
}

// AddAction refuses a blank body with domain.ErrRetroActionBodyRequired -- a
// blank row on the retro detail would look like a rendering bug -- and
// stores the trimmed body.
func (s *RetroService) AddAction(ctx context.Context, in RetroActionInput) (RetroActionRecord, error) {
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" {
		return RetroActionRecord{}, domain.ErrRetroActionBodyRequired
	}
	return s.actions.Add(ctx, in)
}

// SetActionDone ticks or unticks one action, touching only
// RetroActionRepository, never RetroRepository: an action's done state must
// not bump the retro's version, or one partner ticking every action would
// invalidate the other's already-open editor tab for no related reason.
func (s *RetroService) SetActionDone(ctx context.Context, householdID, actionID string, done bool, at time.Time) error {
	return s.actions.SetDone(ctx, householdID, actionID, done, at)
}

// RemoveAction deletes one action. RetroActionRepository.Remove's own
// domain.ErrNotFound on a zero-row match passes through untouched.
func (s *RetroService) RemoveAction(ctx context.Context, householdID, actionID string) error {
	return s.actions.Remove(ctx, householdID, actionID)
}
