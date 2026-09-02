package httpadapter

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The households page and its drill-in. Both are reads inside the /admin
// granted group, so requirePlatformAdmin, auditAdmin, requireCSRF and
// requireAdminGrant apply by construction -- nothing here checks who is
// asking. Every timestamp leaves as RFC 3339 in UTC.

type signupsDTO struct {
	Requested int `json:"requested"`
	Completed int `json:"completed"`
}

type directoryMetricsDTO struct {
	Households         int        `json:"households"`
	ActiveHouseholds7d int        `json:"activeHouseholds7d"`
	Signups30d         signupsDTO `json:"signups30d"`
	PendingInvites     int        `json:"pendingInvites"`
}

type memberMatchDTO struct {
	MemberName  string  `json:"memberName"`
	MemberEmail *string `json:"memberEmail"`
}

type householdListingDTO struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	FamilyName      string          `json:"familyName"`
	MemberCount     int             `json:"memberCount"`
	CreatedAt       time.Time       `json:"createdAt"`
	LastActiveAt    *time.Time      `json:"lastActiveAt"`
	PrimaryCurrency string          `json:"primaryCurrency"`
	Match           *memberMatchDTO `json:"match"`
}

type householdsResponse struct {
	Metrics    directoryMetricsDTO   `json:"metrics"`
	Households []householdListingDTO `json:"households"`
	Truncated  bool                  `json:"truncated"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// handleAdminHouseholds is the list page: metrics and rows in one answer, so
// one page view is one audit row. limit that fails to parse is 0, which the
// service turns into its default -- the operator typed a URL, not a form.
func handleAdminHouseholds(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		overview, err := deps.AdminDirectory.Overview(r.Context(), r.URL.Query().Get("q"), limit)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := householdsResponse{
			Metrics: directoryMetricsDTO{
				Households:         overview.Metrics.Households,
				ActiveHouseholds7d: overview.Metrics.ActiveHouseholds,
				Signups30d:         signupsDTO{Requested: overview.Metrics.SignupsRequested, Completed: overview.Metrics.SignupsCompleted},
				PendingInvites:     overview.Metrics.PendingInvites,
			},
			Households: make([]householdListingDTO, 0, len(overview.Households)),
			Truncated:  overview.Truncated,
		}
		for _, h := range overview.Households {
			dto := householdListingDTO{
				ID: h.ID, Name: h.Name, FamilyName: h.FamilyName, MemberCount: h.MemberCount,
				CreatedAt: h.CreatedAt.UTC(), LastActiveAt: utcPtr(h.LastActiveAt), PrimaryCurrency: h.PrimaryCurrency,
			}
			if h.Match != nil {
				dto.Match = &memberMatchDTO{MemberName: h.Match.Name, MemberEmail: h.Match.Email}
			}
			body.Households = append(body.Households, dto)
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

type householdHeaderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	FamilyName      string    `json:"familyName"`
	CreatedAt       time.Time `json:"createdAt"`
	PrimaryCurrency string    `json:"primaryCurrency"`
}

type householdMemberDTO struct {
	UserID       string     `json:"userId"`
	Name         string     `json:"name"`
	Email        *string    `json:"email"`
	Channel      string     `json:"channel"`
	Role         string     `json:"role"`
	Capabilities []string   `json:"capabilities"`
	LastActiveAt *time.Time `json:"lastActiveAt"`
}

type pendingInviteDTO struct {
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	InvitedByName string    `json:"invitedByName"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type lockoutDTO struct {
	LockedUntil time.Time `json:"lockedUntil"`
}

type householdPageResponse struct {
	Household      householdHeaderDTO   `json:"household"`
	Members        []householdMemberDTO `json:"members"`
	PendingInvites []pendingInviteDTO   `json:"pendingInvites"`
	Lockout        *lockoutDTO          `json:"lockout"`
}

// channelString fails closed: a MemberChannel nobody constructed is an
// error and a 500, never an empty string in the JSON.
func channelString(c usecase.MemberChannel) (string, error) {
	switch c {
	case usecase.ChannelEmail:
		return "email", nil
	case usecase.ChannelTelegram:
		return "telegram", nil
	default:
		return "", fmt.Errorf("unknown member channel %q", c)
	}
}

// handleAdminHousehold is the drill-in. A householdID that is not a UUID is
// refused here, before the service is called: fail closed on a value we did
// not construct. The 404 does not depend on this guard --
// postgres/convert.go's uuid() degrades an unparseable id to the zero UUID,
// whose SELECT matches no row and becomes domain.ErrNotFound anyway. But
// that leniency is also what leaves the flag override *writes* answering 500
// for this input (ADMIN_SURFACE_HANDOVER.md, "Known, deferred"): which way a
// malformed id degrades is a property of a helper two layers down, not of
// this route. Refusing here also skips one SQL round-trip for input that
// can never match.
func handleAdminHousehold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "householdID")
		if _, err := uuid.Parse(id); err != nil {
			MapDomainError(w, r, domain.ErrNotFound)
			return
		}
		page, err := deps.AdminDirectory.Household(r.Context(), id)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		body := householdPageResponse{
			Household: householdHeaderDTO{
				ID: page.ID, Name: page.Name, FamilyName: page.FamilyName,
				CreatedAt: page.CreatedAt.UTC(), PrimaryCurrency: page.PrimaryCurrency,
			},
			Members:        make([]householdMemberDTO, 0, len(page.Members)),
			PendingInvites: make([]pendingInviteDTO, 0, len(page.PendingInvites)),
		}
		for _, m := range page.Members {
			channel, err := channelString(m.Channel)
			if err != nil {
				logAndWriteInternal(w, r, err)
				return
			}
			body.Members = append(body.Members, householdMemberDTO{
				UserID: m.UserID, Name: m.Name, Email: m.Email, Channel: channel,
				Role: string(m.Role), Capabilities: m.Capabilities.Strings(),
				LastActiveAt: utcPtr(m.LastActiveAt),
			})
		}
		for _, i := range page.PendingInvites {
			body.PendingInvites = append(body.PendingInvites, pendingInviteDTO{
				Name: i.Name, Email: i.Email, Role: string(i.Role),
				InvitedByName: i.InvitedByName, ExpiresAt: i.ExpiresAt.UTC(),
			})
		}
		if page.LockedUntil != nil {
			body.Lockout = &lockoutDTO{LockedUntil: page.LockedUntil.UTC()}
		}
		WriteJSON(w, http.StatusOK, body)
	}
}
