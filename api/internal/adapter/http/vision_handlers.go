package httpadapter

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxVisionRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// PUT /marriage/vision/{year}: the body is a whole document -- a theme, a
// description of up to 2000 characters, up to twelve pillars each with their
// own description and eight measures, and twenty-four milestones. 32 KiB
// clears that comfortably while still refusing anything absurd, the same
// reasoning maxRetroRequestBodyBytes gives for its own override.
const maxVisionRequestBodyBytes = 32 * 1024

// measureDTO is one line under a pillar. Kind is "typed", "linked" or
// "broken", and hasFigure is what the screen actually branches on: a broken
// link renders its label with no number at all, so current, target and
// percent are all 0 and must not be read. They are plain ints rather than
// pointers because hasFigure already carries the "there is no figure" state,
// and two ways to say the same thing is one too many.
type measureDTO struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	HasFigure bool   `json:"hasFigure"`
	Current   int    `json:"current"`
	Target    int    `json:"target"`
	Percent   int    `json:"percent"`
	Met       bool   `json:"met"`
	GoalID    string `json:"goalId"`   // "" unless linked
	GoalName  string `json:"goalName"` // "" unless linked and resolved
}

// pillarDTO is one pillar of the vision. Measures is always an array, never
// null, even when empty -- toVisionDTO builds it with make(..., 0, ...),
// never leaves it as a nil range, so the wire always carries "[]".
type pillarDTO struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Measures    []measureDTO `json:"measures"`
}

type milestoneDTO struct {
	Year  int    `json:"year"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

// visionDTO is one household-year. Pillars and Milestones are always arrays,
// never null, even when empty -- the "no null collections" convention every
// DTO in this package follows, so the frontend never distinguishes an absent
// key from an empty one. Version 0 means the year has no vision yet.
type visionDTO struct {
	Year        int            `json:"year"`
	Theme       string         `json:"theme"`
	Description string         `json:"description"`
	Version     int            `json:"version"`
	Pillars     []pillarDTO    `json:"pillars"`
	Milestones  []milestoneDTO `json:"milestones"`
}

type visionResponse struct {
	Vision visionDTO `json:"vision"`
}

// saveVisionRequest is the whole document. There is no per-field "unchanged"
// sentinel: the modal holds every field and sends all of them, which is what
// makes the save a replace rather than a merge.
type saveVisionRequest struct {
	Version     int    `json:"version"`
	Theme       string `json:"theme"`
	Description string `json:"description"`
	Pillars     []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Measures    []struct {
			Label   string `json:"label"`
			Kind    string `json:"kind"`
			Current int    `json:"current"`
			Target  int    `json:"target"`
			GoalID  string `json:"goalId"`
		} `json:"measures"`
	} `json:"pillars"`
	Milestones []struct {
		Year  int    `json:"year"`
		Title string `json:"title"`
		Note  string `json:"note"`
	} `json:"milestones"`
}

// parseVisionYear parses raw as a year and range-checks it against
// domain.MinVisionYear/MaxVisionYear before any caller (service or
// repository) ever sees it. This exists because VisionRepository stores the
// year as a Postgres smallint (int16): a bare strconv.Atoi would let a value
// like 65538 wrap silently to 2 once it reached that cast, so a caller
// asking for a year far out of range would get back year 2's row -- or, on
// GET, an empty document that looks like a legitimate answer -- instead of
// the refusal the spec's own error table requires. Checked here, once, so
// neither handler below can skip it.
func parseVisionYear(raw string) (int, bool) {
	year, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if year < domain.MinVisionYear || year > domain.MaxVisionYear {
		return 0, false
	}
	return year, true
}

func writeInvalidYear(w http.ResponseWriter) {
	WriteError(w, http.StatusUnprocessableEntity, "INVALID_YEAR", "That is not a year.", nil)
}

// handleGetVision returns the composed Vision screen for one household-year,
// defaulting to VisionService.CurrentYear when the caller names none. A year
// nobody has saved comes back 200 with an empty document at version 0
// (VisionService.Get's own doc comment) -- never a 404 -- because the empty
// state IS the page.
func handleGetVision(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		year := deps.Visions.CurrentYear()
		if raw := r.URL.Query().Get("year"); raw != "" {
			parsed, ok := parseVisionYear(raw)
			if !ok {
				writeInvalidYear(w)
				return
			}
			year = parsed
		}

		view, err := deps.Visions.Get(r.Context(), scope.HouseholdID, year)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, visionResponse{Vision: toVisionDTO(view)})
	}
}

// handleSaveVision replaces the whole document under the version guard
// domain.ErrVisionChanged exists for. The year is range-checked here, ahead
// of the service, for the same reason handleGetVision checks it: the
// service does validate the year it is handed (domain.Vision.Validate), but
// this handler must not depend on that downstream layer to catch a
// malformed URL segment -- see parseVisionYear's own doc comment.
func handleSaveVision(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		year, ok := parseVisionYear(chi.URLParam(r, "year"))
		if !ok {
			writeInvalidYear(w)
			return
		}

		var req saveVisionRequest
		if !decodeJSONBodyLimit(w, r, &req, maxVisionRequestBodyBytes) {
			return
		}

		view, err := deps.Visions.Save(r.Context(), scope.HouseholdID, year, toDomainVision(req))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, visionResponse{Vision: toVisionDTO(view)})
	}
}

// toDomainVision maps the request's kind strings onto domain.MeasureKind
// without interpreting them: an unrecognised string passes through as-is and
// is refused by domain.Measure.validate's own default branch. Guessing a kind
// here would move a fail-closed decision into the layer least able to explain
// it.
func toDomainVision(req saveVisionRequest) domain.Vision {
	v := domain.Vision{Theme: req.Theme, Description: req.Description, Version: req.Version}
	for _, p := range req.Pillars {
		pillar := domain.Pillar{Name: p.Name, Description: p.Description}
		for _, m := range p.Measures {
			pillar.Measures = append(pillar.Measures, domain.Measure{
				Label:   m.Label,
				Kind:    domain.MeasureKind(m.Kind),
				Current: m.Current,
				Target:  m.Target,
				GoalID:  m.GoalID,
			})
		}
		v.Pillars = append(v.Pillars, pillar)
	}
	for _, m := range req.Milestones {
		v.Milestones = append(v.Milestones, domain.Milestone{Year: m.Year, Title: m.Title, Note: m.Note})
	}
	return v
}

// toVisionDTO turns the composed usecase view into the wire shape. Every
// slice is built with make(..., 0, ...) rather than left as a nil range
// variable, so an empty pillar's Measures, and a vision with no pillars or
// milestones at all, still serialise as "[]" and never "null" -- the
// property TestGetVisionForANeverSetYearCarriesLiteralEmptyArrays pins on
// the raw wire bytes, not just the decoded length.
func toVisionDTO(view usecase.VisionView) visionDTO {
	pillars := make([]pillarDTO, 0, len(view.Pillars))
	for _, p := range view.Pillars {
		measures := make([]measureDTO, 0, len(p.Measures))
		for _, m := range p.Measures {
			measures = append(measures, measureDTO{
				Label: m.Label, Kind: string(m.Kind), HasFigure: m.HasFigure,
				Current: m.Current, Target: m.Target, Percent: m.Percent,
				Met: m.Met, GoalID: m.GoalID, GoalName: m.GoalName,
			})
		}
		pillars = append(pillars, pillarDTO{Name: p.Name, Description: p.Description, Measures: measures})
	}
	milestones := make([]milestoneDTO, 0, len(view.Milestones))
	for _, m := range view.Milestones {
		milestones = append(milestones, milestoneDTO{Year: m.Year, Title: m.Title, Note: m.Note})
	}
	return visionDTO{
		Year: view.Year, Theme: view.Theme, Description: view.Description,
		Version: view.Version, Pillars: pillars, Milestones: milestones,
	}
}
