package httpadapter

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// categoryDTO's Archived field greys a row out and offers Restore instead
// of Archive on Budget's Edit-categories screen. The transaction modal's
// dropdown shares this shape but only sees archived=false rows, since it
// never sends includeArchived=true.
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

// categoryNameRequest is the wire shape for both Create and Rename: both
// take exactly one field, validated by the same trim-then-refuse-empty rule
// (category.go's validateCategoryName), so one struct is honest rather than
// two identical ones.
type categoryNameRequest struct {
	Name string `json:"name"`
}

// handleListCategories backs the modal's dropdown by default, and Budget's
// "Edit categories" screen with ?includeArchived=true. It's also what
// seeds a household's starter set on first read -- see CategoryService.List
// for why.
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

// handleRenameCategory changes a category's name only. A name collision
// surfaces as 409 CATEGORY_NAME_TAKEN and an id outside this household as
// 404 NOT_FOUND, both untranslated from CategoryService.Rename via
// MapDomainError.
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

// setCategoryArchived backs both the archive and restore routes, the same
// "one function, not two" shape as account_handlers.go's setArchived: a
// rule written twice is a rule fixed once. Neither route decodes a body --
// there's nothing to send beyond the id already in the path.
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
