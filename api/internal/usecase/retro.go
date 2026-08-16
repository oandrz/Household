package usecase

import (
	"context"
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

		month := rec.Retro.Month
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
	retro, err := s.retros.ByMonth(ctx, householdID, month)
	if err != nil {
		return RetroView{}, err
	}

	actions, err := s.actions.ForRetro(ctx, householdID, retro.ID)
	if err != nil {
		return RetroView{}, err
	}

	previous := startOfMonth(month).AddDate(0, -1, 0)
	carryOver, err := s.actions.OpenInMonth(ctx, householdID, previous)
	if err != nil {
		return RetroView{}, err
	}

	return RetroView{Retro: retro, Actions: actions, CarryOver: carryOver}, nil
}
