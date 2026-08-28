package usecase

import (
	"context"
	"errors"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// MeasureView is one line under a pillar, with every figure the screen shows
// already decided. HasFigure false is the whole of the broken-link contract:
// the label renders with a short explanation and NO number, because a zero
// would be a claim -- the same rule Accounts applies when a primary-currency
// change leaves net worth uncomputable. Current, Target and Percent must
// never be read when HasFigure is false; toMeasureView leaves them at their
// zero value on purpose, not as a placeholder.
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
// blind overwrite (Task 8).
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
	clock   Clock
}

func NewVisionService(visions VisionRepository, goals GoalProgressReader, clock Clock) *VisionService {
	return &VisionService{visions: visions, goals: goals, clock: clock}
}

// CurrentYear is the default the handler uses when a request names no year.
// It lives here, not in the handler, so nothing in the HTTP layer computes
// a year of its own.
func (s *VisionService) CurrentYear() int {
	return s.clock.Now().Year()
}

// Get returns the composed Vision screen for one household-year.
func (s *VisionService) Get(ctx context.Context, householdID string, year int) (VisionView, error) {
	v, err := s.visions.Get(ctx, householdID, year)
	if errors.Is(err, domain.ErrNotFound) {
		// A year nobody has saved is not a failure to load one -- the empty
		// state IS the page (spec decision 9). Version 0 travels with it
		// deliberately: it is what tells the next save (Task 8) that this
		// is a create rather than a blind overwrite. Pillars and Milestones
		// are non-nil empty slices, not nil, so the JSON layer (Task 9)
		// serialises "[]" and never "null" for a collection the frontend
		// always expects to range over.
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

// Save validates the draft, then replaces the whole document. The household
// and year come from the caller (the route), never from the body: a request
// that names another household must not be able to write into it, and the
// service is where that is settled rather than in each handler.
func (s *VisionService) Save(ctx context.Context, householdID string, year int, draft domain.Vision) (VisionView, error) {
	// The route's values win over anything the body claims -- this is the
	// one line standing between a request and writing into a household it
	// does not belong to. Set before Validate, so a body naming another
	// household is judged against ITS OWN year/household constraints, not
	// smuggled past them.
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

// toMeasureView decides, once, whether a measure has a figure to show at
// all. Kind arrives from a database column this layer did not construct, so
// the switch fails closed: anything it does not recognise -- including
// domain.MeasureBroken, the shape ON DELETE SET NULL leaves behind -- and
// any linked measure whose goal ProgressByIDs did not return, renders the
// label alone rather than guessing a shape for data it cannot vouch for.
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
		// domain.MeasureBroken, and any Kind this layer has never heard of.
		// Returning here rather than falling into a typed 0/0 or a linked
		// 0% is the whole point of failing closed on a value this layer
		// did not construct.
		return view
	}
	return view
}
