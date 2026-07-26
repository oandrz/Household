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
// "the caller omitted this field" (nil) apart from "the caller sent its
// zero value" (non-nil pointing at false/""). A previous version used plain
// value fields and assigned all six unconditionally: the two cases were
// indistinguishable, so PATCH /household behaved like PUT -- sending the API
// spec's own documented body (which omits secondaryCurrency) blanked it to
// "", which then failed HouseholdService.Update's currency validation with a
// 500, and an omitted fxRateMode would have violated the database's CHECK
// constraint the moment a field actually reached it blank. See the fix
// report for the full account.
type updateHouseholdRequest struct {
	Name                  *string `json:"name"`
	FamilyName            *string `json:"familyName"`
	PrimaryCurrency       *string `json:"primaryCurrency"`
	ShowSecondaryCurrency *bool   `json:"showSecondaryCurrency"`
	SecondaryCurrency     *string `json:"secondaryCurrency"`
	FXRateMode            *string `json:"fxRateMode"`
}

// handleUpdateHousehold reads the current record first and applies only the
// fields present in the request, leaving every omitted field exactly as it
// was -- a real PATCH, not a PUT wearing a PATCH's name. See
// updateHouseholdRequest's doc comment for the bug this fixes.
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

// notificationPreferencesRequest's fields are pointers for the identical
// reason updateHouseholdRequest's are: a plain bool field cannot distinguish
// "the caller didn't mention this toggle" from "the caller explicitly wants
// it off," so a partial PATCH silently switched every omitted preference to
// false. See updateHouseholdRequest's doc comment above for the full account
// of the same bug on /household.
type notificationPreferencesRequest struct {
	BillReminders   *bool `json:"billReminders"`
	OverspendAlerts *bool `json:"overspendAlerts"`
	RetroReminder   *bool `json:"retroReminder"`
	WeeklyDigest    *bool `json:"weeklyDigest"`
}

// handleUpdateNotificationPreferences sits behind requireSession +
// requireCSRF + requireOwner, for the same reason handleUpdateHousehold
// does: these are household-wide toggles on the parents' Settings screen,
// not a per-member preference. GET /notification-preferences stays
// reachable by any authenticated member.
//
// It reads the current preferences first and applies only the toggles
// present in the request, exactly as handleUpdateHousehold does for
// /household -- a real PATCH, not a PUT. Unlike households (always created
// with a row), notification_preferences has no row until the first PATCH
// upserts one (see migrations/00002_identity.sql) -- domain.ErrNotFound from
// the read above is expected on that first call, not an error, and is
// treated as the schema's own column defaults (every toggle DEFAULT true)
// rather than Go's zero value, so a partial first PATCH doesn't silently
// switch every un-mentioned toggle off before the row even exists.
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
