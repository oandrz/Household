package httpadapter

import (
	"encoding/json"
	"net/http"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func handleGetHousehold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		h, err := deps.Households.Get(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toHouseholdDTO(h))
	}
}

type updateHouseholdRequest struct {
	Name                  string `json:"name"`
	FamilyName            string `json:"familyName"`
	PrimaryCurrency       string `json:"primaryCurrency"`
	ShowSecondaryCurrency bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     string `json:"secondaryCurrency"`
	FXRateMode            string `json:"fxRateMode"`
}

// handleUpdateHousehold reads the current record first and overwrites it
// with the request's fields, rather than trusting the caller to submit a
// complete domain.Household: HouseholdService.Update persists every field on
// what it's handed (see its doc comment in usecase/household.go), so a
// request that omitted a field would otherwise blank it out.
//
// This sits behind requireSession + requireCSRF + requireOwner: the
// household's primary/secondary currency and FX mode are household-wide
// settings the design presents on the parents' Settings screen, and letting
// a limited member (a child) change them was an authorization hole the
// initial routing left open -- see the task-16 fix report's audit for the
// full route-by-route reasoning. GET /household stays reachable by any
// authenticated member: the frontend needs these values to render amounts
// for anyone who can see a money figure, and reading them discloses nothing
// a member doesn't already see on screen.
func handleUpdateHousehold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req updateHouseholdRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		current, err := deps.Households.Get(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		current.Name = req.Name
		current.FamilyName = req.FamilyName
		current.PrimaryCurrency = req.PrimaryCurrency
		current.ShowSecondaryCurrency = req.ShowSecondaryCurrency
		current.SecondaryCurrency = req.SecondaryCurrency
		current.FXRateMode = req.FXRateMode

		updated, err := deps.Households.Update(r.Context(), current)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toHouseholdDTO(updated))
	}
}

func handleListSpaces(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		spaces, err := deps.Households.Spaces(r.Context(), scope.HouseholdID, scope.Membership)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toSpaceDTOs(spaces))
	}
}

type createSpaceRequest struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	// Template is accepted because the API spec's request shape names it,
	// but it is otherwise unused: HouseholdService.CreateSpace has no notion
	// of a space template, only a name and a visibility.
	Template string `json:"template"`
}

// handleCreateSpace sits behind requireOwner: only an owner may add a
// custom space to the household's sidebar.
func handleCreateSpace(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req createSpaceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		created, err := deps.Households.CreateSpace(r.Context(), scope.HouseholdID, req.Name, domain.Visibility(req.Visibility))
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, toSpaceDTO(created))
	}
}

type notificationPreferencesDTO struct {
	BillReminders   bool `json:"billReminders"`
	OverspendAlerts bool `json:"overspendAlerts"`
	RetroReminder   bool `json:"retroReminder"`
	WeeklyDigest    bool `json:"weeklyDigest"`
}

func toNotificationPreferencesDTO(p usecase.NotificationPreferences) notificationPreferencesDTO {
	return notificationPreferencesDTO{
		BillReminders:   p.BillReminders,
		OverspendAlerts: p.OverspendAlerts,
		RetroReminder:   p.RetroReminder,
		WeeklyDigest:    p.WeeklyDigest,
	}
}

func handleGetNotificationPreferences(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		p, err := deps.Households.Notifications(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toNotificationPreferencesDTO(p))
	}
}

type notificationPreferencesRequest struct {
	BillReminders   bool `json:"billReminders"`
	OverspendAlerts bool `json:"overspendAlerts"`
	RetroReminder   bool `json:"retroReminder"`
	WeeklyDigest    bool `json:"weeklyDigest"`
}

// handleUpdateNotificationPreferences sits behind requireSession +
// requireCSRF + requireOwner, for the same reason handleUpdateHousehold
// does: these are household-wide toggles on the parents' Settings screen,
// not a per-member preference. GET /notification-preferences stays
// reachable by any authenticated member.
func handleUpdateNotificationPreferences(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req notificationPreferencesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_BODY", "The request body could not be parsed.", nil)
			return
		}
		updated, err := deps.Households.UpdateNotifications(r.Context(), scope.HouseholdID, usecase.NotificationPreferences{
			BillReminders:   req.BillReminders,
			OverspendAlerts: req.OverspendAlerts,
			RetroReminder:   req.RetroReminder,
			WeeklyDigest:    req.WeeklyDigest,
		})
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toNotificationPreferencesDTO(updated))
	}
}
