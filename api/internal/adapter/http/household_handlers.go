package httpadapter

import (
	"errors"
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

// updateHouseholdRequest's fields are all pointers so the handler can tell
// "the caller omitted this field" apart from "the caller sent its zero
// value". Don't switch back to plain value fields: doing that once made
// PATCH /household behave like PUT, blanking secondaryCurrency and
// violating fxRateMode's CHECK constraint on any request that omitted them.
type updateHouseholdRequest struct {
	Name                  *string `json:"name"`
	FamilyName            *string `json:"familyName"`
	PrimaryCurrency       *string `json:"primaryCurrency"`
	ShowSecondaryCurrency *bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     *string `json:"secondaryCurrency"`
	FXRateMode            *string `json:"fxRateMode"`
	Timezone              *string `json:"timezone"`
}

// handleUpdateHousehold reads the current record and applies only the
// fields present in the request -- a real PATCH, not a PUT (see
// updateHouseholdRequest for the bug that shape avoids).
//
// It sits behind requireSession + requireCSRF + requireOwner: currency and
// FX settings are household-wide, on the parents' Settings screen, and a
// limited member (a child) must not change them. GET /household stays open
// to any authenticated member -- it renders amounts for anyone who can see
// a money figure, and discloses nothing they can't already see on screen.
func handleUpdateHousehold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req updateHouseholdRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		current, err := deps.Households.Get(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		if req.Name != nil {
			current.Name = *req.Name
		}
		if req.FamilyName != nil {
			current.FamilyName = *req.FamilyName
		}
		if req.PrimaryCurrency != nil {
			current.PrimaryCurrency = *req.PrimaryCurrency
		}
		if req.ShowSecondaryCurrency != nil {
			current.ShowSecondaryCurrency = *req.ShowSecondaryCurrency
		}
		if req.SecondaryCurrency != nil {
			current.SecondaryCurrency = *req.SecondaryCurrency
		}
		if req.FXRateMode != nil {
			current.FXRateMode = *req.FXRateMode
		}
		if req.Timezone != nil {
			current.Timezone = *req.Timezone
		}

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
		if !decodeJSONBody(w, r, &req) {
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

// notificationPreferencesRequest's fields are pointers for the same reason
// updateHouseholdRequest's are (see its doc comment): a plain bool can't
// tell "not mentioned" from "explicitly turned off," and treating it as
// the latter would silently flip every omitted toggle to false.
type notificationPreferencesRequest struct {
	BillReminders   *bool `json:"billReminders"`
	OverspendAlerts *bool `json:"overspendAlerts"`
	RetroReminder   *bool `json:"retroReminder"`
	WeeklyDigest    *bool `json:"weeklyDigest"`
}

// handleUpdateNotificationPreferences sits behind requireSession +
// requireCSRF + requireOwner, same reason as handleUpdateHousehold: these
// are household-wide toggles on the parents' Settings screen, not a
// per-member preference. GET /notification-preferences stays open to any
// authenticated member.
//
// It reads the current row and applies only the toggles present in the
// request -- the same PATCH-not-PUT shape as handleUpdateHousehold.
// notification_preferences has no row until the first PATCH upserts one
// (migrations/00002_identity.sql), so domain.ErrNotFound on that first read
// is expected, not an error: it's filled with the schema's own defaults
// (every toggle DEFAULT true), not Go's zero value, so a first partial
// PATCH can't silently turn every un-mentioned toggle off.
func handleUpdateNotificationPreferences(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req notificationPreferencesRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		current, err := deps.Households.Notifications(r.Context(), scope.HouseholdID)
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) {
				MapDomainError(w, r, err)
				return
			}
			current = usecase.NotificationPreferences{
				BillReminders: true, OverspendAlerts: true, RetroReminder: true, WeeklyDigest: true,
			}
		}
		if req.BillReminders != nil {
			current.BillReminders = *req.BillReminders
		}
		if req.OverspendAlerts != nil {
			current.OverspendAlerts = *req.OverspendAlerts
		}
		if req.RetroReminder != nil {
			current.RetroReminder = *req.RetroReminder
		}
		if req.WeeklyDigest != nil {
			current.WeeklyDigest = *req.WeeklyDigest
		}

		updated, err := deps.Households.UpdateNotifications(r.Context(), scope.HouseholdID, current)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, toNotificationPreferencesDTO(updated))
	}
}
