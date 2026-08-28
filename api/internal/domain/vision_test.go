package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

func validVision() domain.Vision {
	return domain.Vision{
		HouseholdID: "h1",
		Year:        2026,
		Theme:       "Slow down together",
		Description: "Fewer commitments, more presence.",
		Pillars: []domain.Pillar{{
			Name:        "Us before logistics",
			Description: "We're partners first.",
			Measures: []domain.Measure{
				{Label: "Date nights / month", Kind: domain.MeasureTyped, Current: 2, Target: 2},
				{Label: "Emergency fund", Kind: domain.MeasureLinked, GoalID: "g1"},
			},
		}},
		Milestones: []domain.Milestone{{Year: 2029, Title: "Upgrade to a bigger place"}},
	}
}

func TestAValidVisionValidates(t *testing.T) {
	if err := validVision().Validate(); err != nil {
		t.Fatalf("expected the fixture to be valid, got %v", err)
	}
}

func TestVisionValidationRefuses(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*domain.Vision)
		want   error
	}{
		{"an empty theme", func(v *domain.Vision) { v.Theme = "   " }, domain.ErrVisionThemeRequired},
		{"an over-long theme", func(v *domain.Vision) { v.Theme = strings.Repeat("x", 121) }, domain.ErrVisionThemeTooLong},
		{"an over-long description", func(v *domain.Vision) { v.Description = strings.Repeat("x", 2001) }, domain.ErrVisionDescriptionTooLong},
		{"a year below the range", func(v *domain.Vision) { v.Year = 1899 }, domain.ErrVisionYearOutOfRange},
		{"a year above the range", func(v *domain.Vision) { v.Year = 2201 }, domain.ErrVisionYearOutOfRange},
		{"a nameless pillar", func(v *domain.Vision) { v.Pillars[0].Name = "" }, domain.ErrVisionPillarNameRequired},
		{"an unlabelled measure", func(v *domain.Vision) { v.Pillars[0].Measures[0].Label = "" }, domain.ErrVisionMeasureLabelRequired},
		{"a typed measure with a zero target", func(v *domain.Vision) { v.Pillars[0].Measures[0].Target = 0 }, domain.ErrVisionMeasureTargetNotPositive},
		{"a typed measure with a negative current", func(v *domain.Vision) { v.Pillars[0].Measures[0].Current = -1 }, domain.ErrVisionMeasureCurrentNegative},
		{"a typed measure that also names a goal", func(v *domain.Vision) { v.Pillars[0].Measures[0].GoalID = "g9" }, domain.ErrVisionMeasureAmbiguous},
		{"a linked measure that also carries a target", func(v *domain.Vision) { v.Pillars[0].Measures[1].Target = 5 }, domain.ErrVisionMeasureAmbiguous},
		{"a linked measure naming no goal", func(v *domain.Vision) { v.Pillars[0].Measures[1].GoalID = "" }, domain.ErrVisionMeasureGoalRequired},
		// The database tolerates a broken link because ON DELETE SET NULL
		// produces one. A save must never create one.
		{"a broken measure on the write path", func(v *domain.Vision) { v.Pillars[0].Measures[1].Kind = domain.MeasureBroken }, domain.ErrVisionMeasureAmbiguous},
		{"an unknown measure kind", func(v *domain.Vision) { v.Pillars[0].Measures[1].Kind = "sideways" }, domain.ErrVisionMeasureAmbiguous},
		{"a titleless milestone", func(v *domain.Vision) { v.Milestones[0].Title = " " }, domain.ErrVisionMilestoneTitleRequired},
		{"a milestone year out of range", func(v *domain.Vision) { v.Milestones[0].Year = 3000 }, domain.ErrVisionYearOutOfRange},
		{"too many pillars", func(v *domain.Vision) {
			v.Pillars = make([]domain.Pillar, domain.MaxVisionPillars+1)
			for i := range v.Pillars {
				v.Pillars[i] = domain.Pillar{Name: "P"}
			}
		}, domain.ErrVisionTooManyPillars},
		{"too many measures on one pillar", func(v *domain.Vision) {
			v.Pillars[0].Measures = make([]domain.Measure, domain.MaxPillarMeasures+1)
			for i := range v.Pillars[0].Measures {
				v.Pillars[0].Measures[i] = domain.Measure{Label: "M", Kind: domain.MeasureTyped, Target: 1}
			}
		}, domain.ErrVisionTooManyMeasures},
		{"too many milestones", func(v *domain.Vision) {
			v.Milestones = make([]domain.Milestone, domain.MaxVisionMilestones+1)
			for i := range v.Milestones {
				v.Milestones[i] = domain.Milestone{Year: 2030, Title: "M"}
			}
		}, domain.ErrVisionTooManyMilestones},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validVision()
			tc.mutate(&v)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// TestVisionThemeCapCountsRunesNotBytes pins the fix for a defect in the
// original brief: MaxVisionThemeLen is a character count ("≤ 120 chars" in
// the spec), so the cap must be checked with utf8.RuneCountInString, not
// len(). A household writing its theme in Chinese -- Hearth is built for a
// Singapore household -- would otherwise be capped at a third of what the
// product promises, since each such character costs three bytes in UTF-8.
func TestVisionThemeCapCountsRunesNotBytes(t *testing.T) {
	t.Run("120 multi-byte characters is exactly at the cap and passes", func(t *testing.T) {
		v := validVision()
		v.Theme = strings.Repeat("好", domain.MaxVisionThemeLen)
		if err := v.Validate(); err != nil {
			t.Fatalf("expected a 120-character theme to validate, got %v", err)
		}
	})

	t.Run("121 multi-byte characters is over the cap and is refused", func(t *testing.T) {
		v := validVision()
		v.Theme = strings.Repeat("好", domain.MaxVisionThemeLen+1)
		if err := v.Validate(); !errors.Is(err, domain.ErrVisionThemeTooLong) {
			t.Fatalf("want %v, got %v", domain.ErrVisionThemeTooLong, err)
		}
	})
}
