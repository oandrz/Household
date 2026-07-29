package httpadapter

import "net/http"

type categoryDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type categoriesResponse struct {
	Categories []categoryDTO `json:"categories"`
}

// handleListCategories is the modal's dropdown. It is also what seeds a
// household's starter set the first time anything asks -- see
// CategoryService.List for why a read is the moment that does it.
func handleListCategories(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		categories, err := deps.Categories.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := categoriesResponse{Categories: make([]categoryDTO, 0, len(categories))}
		for _, c := range categories {
			out.Categories = append(out.Categories, categoryDTO{
				ID: c.ID, Name: c.Name, Kind: string(c.Kind),
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}
