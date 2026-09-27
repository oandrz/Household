package domain

import (
	"strings"
	"unicode/utf8"
)

// The collection caps bound one save's cost -- a save rewrites every
// child of a document, so cost can't depend on request-body size.
// Generous against a three-pillar, three-milestone design.
const (
	MaxVisionPillars        = 12
	MaxPillarMeasures       = 8
	MaxVisionMilestones     = 24
	MaxVisionThemeLen       = 120
	MaxVisionDescriptionLen = 2000
	MinVisionYear           = 1900
	MaxVisionYear           = 2200
)

// MeasureKind is which of three shapes a measure has. MeasureBroken comes
// from ON DELETE SET NULL on a deleted linked goal: rendered as a label
// with no figure, but refused by Validate on the write path.
type MeasureKind string

const (
	MeasureTyped  MeasureKind = "typed"
	MeasureLinked MeasureKind = "linked"
	MeasureBroken MeasureKind = "broken"
)

// Measure is one line under a pillar. A typed measure carries Current and
// Target; a linked one carries GoalID and reads its figure from that goal.
type Measure struct {
	ID      string
	Label   string
	Kind    MeasureKind
	Current int
	Target  int
	GoalID  string
}

type Pillar struct {
	ID          string
	Name        string
	Description string
	Measures    []Measure
}

type Milestone struct {
	ID    string
	Year  int
	Title string
	Note  string
}

// Vision is one household-year. Version is the optimistic-concurrency token:
// 0 means "read from a year that had no vision", so a save carrying 0 is a
// create.
type Vision struct {
	ID          string
	HouseholdID string
	Year        int
	Theme       string
	Description string
	Version     int
	Pillars     []Pillar
	Milestones  []Milestone
}

// Validate is the write path's rules, stricter than the database:
// MeasureBroken passes the schema's CHECK branch but is refused here, so
// a measure can't be created with a missing figure on purpose.
func (v Vision) Validate() error {
	if strings.TrimSpace(v.Theme) == "" {
		return ErrVisionThemeRequired
	}
	// Rune count, not len: len is bytes, and a theme written in Chinese (this
	// product is built for a Singapore household) would otherwise be capped
	// at a third of what the product promises.
	if utf8.RuneCountInString(v.Theme) > MaxVisionThemeLen {
		return ErrVisionThemeTooLong
	}
	// Same reasoning as the theme check above: count runes, not bytes.
	if utf8.RuneCountInString(v.Description) > MaxVisionDescriptionLen {
		return ErrVisionDescriptionTooLong
	}
	if v.Year < MinVisionYear || v.Year > MaxVisionYear {
		return ErrVisionYearOutOfRange
	}
	if len(v.Pillars) > MaxVisionPillars {
		return ErrVisionTooManyPillars
	}
	for _, p := range v.Pillars {
		if err := p.validate(); err != nil {
			return err
		}
	}
	if len(v.Milestones) > MaxVisionMilestones {
		return ErrVisionTooManyMilestones
	}
	for _, m := range v.Milestones {
		if strings.TrimSpace(m.Title) == "" {
			return ErrVisionMilestoneTitleRequired
		}
		if m.Year < MinVisionYear || m.Year > MaxVisionYear {
			return ErrVisionYearOutOfRange
		}
	}
	return nil
}

func (p Pillar) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrVisionPillarNameRequired
	}
	if len(p.Measures) > MaxPillarMeasures {
		return ErrVisionTooManyMeasures
	}
	for _, m := range p.Measures {
		if err := m.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (m Measure) validate() error {
	if strings.TrimSpace(m.Label) == "" {
		return ErrVisionMeasureLabelRequired
	}
	// Fail closed: Kind arrives from a request body, so anything this switch
	// does not recognise -- MeasureBroken included -- is refused rather than
	// falling through to a default shape.
	switch m.Kind {
	case MeasureTyped:
		if m.GoalID != "" {
			return ErrVisionMeasureAmbiguous
		}
		if m.Target <= 0 {
			return ErrVisionMeasureTargetNotPositive
		}
		if m.Current < 0 {
			return ErrVisionMeasureCurrentNegative
		}
		return nil
	case MeasureLinked:
		if m.Current != 0 || m.Target != 0 {
			return ErrVisionMeasureAmbiguous
		}
		if m.GoalID == "" {
			return ErrVisionMeasureGoalRequired
		}
		return nil
	default:
		return ErrVisionMeasureAmbiguous
	}
}
