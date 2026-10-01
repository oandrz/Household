package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// MeasureView is one line under a pillar, every figure pre-decided.
// HasFigure false is the whole broken-link contract: label and short
// explanation, no number -- a zero would be a false claim (the same rule
// Accounts applies to an uncomputable net worth). Current, Target and
// Percent then sit at zero on purpose, and must never be read.
type MeasureView struct {
	Label     string
	Kind      domain.MeasureKind
	HasFigure bool
	Current   int // typed only
	Target    int // typed only
	Percent   int // linked only -- domain.GoalProgressPercent's own figure, not recomputed here
	Met       bool
	GoalID    string
	GoalName  string
}

type PillarView struct {
	Name        string
	Description string
	Measures    []MeasureView
}

// VisionView is the whole screen in one response. Version travels with it
// because every save must send back the version it read -- 0 for a year
// that had none, which is what makes the first save a create rather than a
// blind overwrite.
type VisionView struct {
	Year        int
	Theme       string
	Description string
	Version     int
	Pillars     []PillarView
	Milestones  []domain.Milestone
}

// VisionService composes the Vision screen and is its one write path (Save).
type VisionService struct {
	visions VisionRepository
	goals   GoalProgressReader
}

func NewVisionService(visions VisionRepository, goals GoalProgressReader) *VisionService {
	return &VisionService{visions: visions, goals: goals}
}

// CurrentYear is the default the handler uses when a request names no year:
// the year of the household's calendar day, which the caller passes in. It
// lives here, not in the handler, so nothing in the HTTP layer decides what
// "this year" means.
func (s *VisionService) CurrentYear(today time.Time) int {
	return today.Year()
}

// Get returns the composed Vision screen for one household-year.
func (s *VisionService) Get(ctx context.Context, householdID string, year int) (VisionView, error) {
	v, err := s.visions.Get(ctx, householdID, year)
	if errors.Is(err, domain.ErrNotFound) {
		// A year nobody has saved is not a failure -- the empty state IS the
		// page. Version 0 tells the next save this is a create, not an
		// overwrite. Pillars/Milestones are non-nil empty slices, so JSON
		// serialises "[]", never "null", for what the frontend ranges over.
		return VisionView{Year: year, Version: 0, Pillars: []PillarView{}, Milestones: []domain.Milestone{}}, nil
	}
	if err != nil {
		return VisionView{}, err
	}
	return s.compose(ctx, householdID, v)
}

// compose resolves every linked measure's goal in one call, rather than one
// round trip per measure, and turns the domain document into the view the
// screen renders.
func (s *VisionService) compose(ctx context.Context, householdID string, v domain.Vision) (VisionView, error) {
	progress, err := s.goals.ProgressByIDs(ctx, householdID, linkedGoalIDs(v))
	if err != nil {
		return VisionView{}, err
	}

	pillars := make([]PillarView, 0, len(v.Pillars))
	for _, p := range v.Pillars {
		measures := make([]MeasureView, 0, len(p.Measures))
		for _, m := range p.Measures {
			measures = append(measures, toMeasureView(m, progress))
		}
		pillars = append(pillars, PillarView{Name: p.Name, Description: p.Description, Measures: measures})
	}

	milestones := v.Milestones
	if milestones == nil {
		milestones = []domain.Milestone{}
	}
	return VisionView{
		Year: v.Year, Theme: v.Theme, Description: v.Description,
		Version: v.Version, Pillars: pillars, Milestones: milestones,
	}, nil
}

// Save validates the draft, then replaces the whole document. Household
// and year come from the route, never the body -- so a request naming
// another household can't write into it, settled here, not per handler.
func (s *VisionService) Save(ctx context.Context, householdID string, year int, draft domain.Vision) (VisionView, error) {
	// Set before Validate, so a body naming another household is judged
	// against ITS OWN constraints, not smuggled past them.
	draft.HouseholdID = householdID
	draft.Year = year

	if err := draft.Validate(); err != nil {
		return VisionView{}, err
	}

	saved, err := s.visions.Save(ctx, draft)
	if err != nil {
		return VisionView{}, err
	}
	return s.compose(ctx, householdID, saved)
}

// linkedGoalIDs collects every goal a measure in this vision points at, so
// compose can resolve them all in the one ProgressByIDs call the port's own
// doc comment calls for, instead of one query per measure.
func linkedGoalIDs(v domain.Vision) []string {
	var ids []string
	for _, p := range v.Pillars {
		for _, m := range p.Measures {
			if m.Kind == domain.MeasureLinked && m.GoalID != "" {
				ids = append(ids, m.GoalID)
			}
		}
	}
	return ids
}

// toMeasureView decides, once, whether a measure has a figure to show.
// Kind is untrusted (a database column this layer didn't construct), so an
// unrecognised Kind (domain.MeasureBroken, from ON DELETE SET NULL) or an
// unresolved linked goal renders the label alone, never a guessed shape.
func toMeasureView(m domain.Measure, progress map[string]GoalProgress) MeasureView {
	view := MeasureView{Label: m.Label, Kind: m.Kind}
	switch m.Kind {
	case domain.MeasureTyped:
		view.HasFigure = true
		view.Current, view.Target = m.Current, m.Target
		view.Met = m.Current >= m.Target
	case domain.MeasureLinked:
		view.GoalID = m.GoalID
		found, ok := progress[m.GoalID]
		if !ok {
			// The goal was deleted between this vision being saved and this
			// read, or the link never resolved. Label only -- no number.
			return view
		}
		view.HasFigure = true
		view.Percent = found.Percent
		view.GoalName = found.Name
		view.Met = found.Percent >= 100
	default:
		// domain.MeasureBroken and any unknown Kind land here, not a typed
		// 0/0 or linked 0% -- the whole point of failing closed on this.
		return view
	}
	return view
}
