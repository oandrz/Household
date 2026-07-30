package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// categoryDTO's Archived field is what Budget's "Edit categories" screen
// reads to grey a row out and offer Restore instead of Archive; the
// transaction modal's dropdown reads the same shape but only ever sees rows
// where it is false, since that request never sends includeArchived=true.
type categoryDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Archived bool   `json:"archived"`
}

func toCategoryDTO(c domain.Category) categoryDTO {
	return categoryDTO{ID: c.ID, Name: c.Name, Kind: string(c.Kind), Archived: c.IsArchived()}
}

type categoriesResponse struct {
	Categories []categoryDTO `json:"categories"`
}

// categoryResponse is the {"category": {...}} shape every write route below
// answers, matched by budget_api_test.go's categoryBody.
type categoryResponse struct {
	Category categoryDTO `json:"category"`
}

// categoryNameRequest is the wire shape for both Create and Rename -- the
// two routes take exactly one field, and CategoryService validates it with
// the same trim-then-refuse-empty rule for both (category.go's
// validateCategoryName), so one request struct is honest rather than two
// identical ones.
type categoryNameRequest struct {
	Name string `json:"name"`
}

// handleListCategories is the modal's dropdown by default, and Budget's
// "Edit categories" screen with ?includeArchived=true. It is also what seeds
// a household's starter set the first time anything asks -- see
// CategoryService.List for why a read is the moment that does it.
func handleListCategories(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		includeArchived := r.URL.Query().Get("includeArchived") == "true"
		categories, err := deps.Categories.ListFiltered(r.Context(), scope.HouseholdID, includeArchived)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := categoriesResponse{Categories: make([]categoryDTO, 0, len(categories))}
		for _, c := range categories {
			out.Categories = append(out.Categories, toCategoryDTO(c))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleCreateCategory adds one category to the household's list. It is
// always CategoryExpense -- see CategoryService.Create's own comment for why
// the write path takes no kind argument at all.
func handleCreateCategory(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req categoryNameRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		created, err := deps.Categories.Create(r.Context(), scope.HouseholdID, req.Name)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, categoryResponse{Category: toCategoryDTO(created)})
	}
}

// handleRenameCategory changes a category's name only. A name colliding with
// another row surfaces as 409 CATEGORY_NAME_TAKEN; an id outside this
// household surfaces as 404 NOT_FOUND -- both untranslated from
// CategoryService.Rename, through MapDomainError.
func handleRenameCategory(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req categoryNameRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		id := chi.URLParam(r, "id")
		renamed, err := deps.Categories.Rename(r.Context(), scope.HouseholdID, id, req.Name)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, categoryResponse{Category: toCategoryDTO(renamed)})
	}
}

func handleArchiveCategory(deps Deps) http.HandlerFunc { return setCategoryArchived(deps, true) }
func handleRestoreCategory(deps Deps) http.HandlerFunc { return setCategoryArchived(deps, false) }

// setCategoryArchived backs both the archive and the restore route, the same
// "one function, not two near-identical ones" shape account_handlers.go's
// setArchived uses -- the pair differ by a single boolean, and a rule
// written twice is a rule fixed once. Neither route decodes a body: there is
// nothing to send beyond the id already in the path.
func setCategoryArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")

		var (
			category domain.Category
			err      error
		)
		if archived {
			category, err = deps.Categories.Archive(r.Context(), scope.HouseholdID, id)
		} else {
			category, err = deps.Categories.Restore(r.Context(), scope.HouseholdID, id)
		}
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, categoryResponse{Category: toCategoryDTO(category)})
	}
}
