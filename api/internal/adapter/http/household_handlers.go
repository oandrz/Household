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
// This sits behind requireSession + requireCSRF only, per the task brief's
// routing description, which names requireOwner explicitly for
// /household/members mutations and /spaces creation but not for this route
// or notification-preferences. That leaves any member -- including a
// limited one -- able to edit household-wide settings such as the primary
// currency. Flagged in the task report as an authorization gap for the
// coordinator to rule on; not something this task invents a guard for on its
// own authority.
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

// handleCreateSpace sits behind requireOwner, per the task brief.
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
// requireCSRF only -- see handleUpdateHousehold's doc comment for why that
// matches the task brief but leaves a limited member able to change these
// too.
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
