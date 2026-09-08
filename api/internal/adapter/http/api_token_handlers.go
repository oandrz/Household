package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// apiTokenDTO is a token as a list shows it. There is deliberately no
// field for the secret: the raw token appears in createdAPITokenResponse
// once and nowhere else.
type apiTokenDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func toAPITokenDTO(t domain.APIToken) apiTokenDTO {
	return apiTokenDTO{ID: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt}
}

type createAPITokenRequest struct {
	Name string `json:"name"`
	// ExpiresInDays zero or absent means the default (90). Capped at 365.
	ExpiresInDays int `json:"expiresInDays"`
}

type createdAPITokenResponse struct {
	apiTokenDTO
	// Token is the raw secret, shown exactly once. The caller stores it;
	// the server cannot show it again.
	Token string `json:"token"`
}

func handleListAPITokens(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		tokens, err := deps.APITokens.List(r.Context(), scope.UserID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]apiTokenDTO, 0, len(tokens))
		for _, t := range tokens {
			out = append(out, toAPITokenDTO(t))
		}
		WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
	}
}

func handleCreateAPIToken(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createAPITokenRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.ExpiresInDays < 0 {
			MapDomainError(w, r, domain.ErrAPITokenLifetimeInvalid)
			return
		}
		created, err := deps.APITokens.Create(r.Context(), usecase.NewAPITokenInput{
			UserID:      scope.UserID,
			HouseholdID: scope.HouseholdID,
			Name:        req.Name,
			Lifetime:    time.Duration(req.ExpiresInDays) * 24 * time.Hour,
		})
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, createdAPITokenResponse{apiTokenDTO: toAPITokenDTO(created.Token), Token: created.Raw})
	}
}

func handleRevokeAPIToken(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		if err := deps.APITokens.Revoke(r.Context(), scope.UserID, chi.URLParam(r, "id")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
