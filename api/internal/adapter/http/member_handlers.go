package httpadapter

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type memberViewDTO struct {
	ID           string   `json:"id"`
	User         userDTO  `json:"user"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// toMemberViewDTO builds one row of the member list. revealEmail gates
// User.Email: false empties it rather than omitting it (userDTO.Email has
// no `omitempty`), so the response shape stays identical either way and the
// frontend needs only one type.
//
// An empty email here is deliberately ambiguous between "withheld" and
// "this member genuinely has none" (a credential-less child's own record)
// -- see the per-field redaction in handleListMembers.
func toMemberViewDTO(v usecase.MemberView, revealEmail bool) memberViewDTO {
	user := toUserDTO(v.User)
	if !revealEmail {
		user.Email = ""
	}
	return memberViewDTO{
		ID:           v.Membership.ID,
		User:         user,
		Role:         string(v.Membership.Role),
		Capabilities: v.Membership.Capabilities.Strings(),
	}
}

// handleListMembers is reachable by any authenticated member: names, roles
// and capabilities are needed household-wide (payer and assignee pickers
// across money, marriage and overview read them). Emails are personal
// data and the auth surface's own identifier, so nothing in the design
// shows a child a parent's email -- the roster always lists every member,
// but only an owner gets User.Email populated; a limited caller sees it
// emptied per row (toMemberViewDTO's revealEmail). This redacts per
// field, not per row, so a limited caller still learns who is in the
// household and what they can do, just not how to reach them by email.
func handleListMembers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		views, err := deps.Members.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		revealEmail := scope.Membership.Role == domain.RoleOwner
		out := make([]memberViewDTO, 0, len(views))
		for _, v := range views {
			out = append(out, toMemberViewDTO(v, revealEmail))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type inviteMemberRequest struct {
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
	Channel      string   `json:"channel"`
}

// inviteCreatedDTO answers every channel, with fields omitted when absent
// rather than sent as a zero value: an email invite has no id to report
// (Create predates this shape), and a profile or email invite has no
// expiry or link -- serialising ExpiresAt as Go's zero time would print a
// real-looking but false "0001-01-01T00:00:00Z". ID and ExpiresAt are
// pointers because `omitempty` cannot suppress a zero time.Time; Link is
// already a string, so its own "" is enough. Every 2xx except 204 still
// carries a body (CLAUDE.md): the profile and email arms answer `{}`.
type inviteCreatedDTO struct {
	ID        *string    `json:"id,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Link      string     `json:"link,omitempty"`
}

// An inviteChannelChoice is how the person being added will sign in, as the
// request says it. It has one value more than domain.InviteChannel, and the
// extra one is the point: "profile" means they never sign in at all, so no
// invite row is written and there is no channel to store. Keeping it out of
// the domain type keeps that type equal to what the column holds.
type inviteChannelChoice string

const (
	channelChoiceProfile  inviteChannelChoice = "profile"
	channelChoiceEmail    inviteChannelChoice = "email"
	channelChoiceTelegram inviteChannelChoice = "telegram"
)

// parseInviteChannelChoice refuses anything else, "" included -- which is
// what an omitted field decodes to. A caller must say how this person signs
// in; inferring it from which other fields happen to be filled in is how an
// invite goes somewhere nobody meant.
func parseInviteChannelChoice(s string) (inviteChannelChoice, error) {
	switch inviteChannelChoice(s) {
	case channelChoiceProfile:
		return channelChoiceProfile, nil
	case channelChoiceEmail:
		return channelChoiceEmail, nil
	case channelChoiceTelegram:
		return channelChoiceTelegram, nil
	default:
		return "", fmt.Errorf("%w: %q", domain.ErrUnknownInviteChannel, s)
	}
}

// handleInviteMember sits behind requireOwner: only an owner may add a
// member. It parses role, capabilities and channel itself (rather than
// handing raw strings to the service) so a malformed value is reported
// through the same MapDomainError table (INVALID_ROLE / INVALID_CAPABILITIES
// / INVALID_INVITE_CHANNEL) that a value domain.NewMembership itself rejects
// would be.
func handleInviteMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		var req inviteMemberRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		role, err := domain.ParseRole(req.Role)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		caps, err := domain.ParseCapabilities(req.Capabilities)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		choice, err := parseInviteChannelChoice(req.Channel)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		// The flag is enforced here as well as in the modal, because this
		// route is reachable from hearthctl and anything else holding a
		// session: an invite that can never be delivered must not be
		// creatable at all. adminctl is deliberately outside this gate --
		// it calls InviteService directly and prints the URL it captures,
		// which is how an operator hands an invite over today.
		//
		// Every arm below answers and returns; don't let one fall through
		// to a shared call after the switch. That shape once let a
		// {"channel":"email","email":""} request slip past the flag check
		// into Create's own empty-email branch, silently creating a
		// profile-only member with no invite row, no token and no mail for
		// a caller who asked for email.
		switch choice {
		case channelChoiceProfile:
			// Today's kid path, untouched: Create's own empty-email branch
			// creates the member directly and writes no invite row. It
			// refuses any role but limited, which is the check this arm
			// deliberately does not repeat -- one rule, one place.
			if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, "", role, caps); err != nil {
				MapDomainError(w, r, err)
				return
			}
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{})
		case channelChoiceEmail:
			if !scope.Flags.Enabled(domain.FlagEmailInvites) {
				MapDomainError(w, r, domain.ErrEmailInvitesDisabled)
				return
			}
			// A caller who asked for the email channel but sent no address
			// is refused here, not handed to Create: its empty-email
			// branch exists for the profile arm's kid case, and would
			// otherwise silently create a profile-only member with no
			// invite, token or mail for a request that asked for one.
			if req.Email == "" {
				MapDomainError(w, r, domain.ErrInviteRequiresEmail)
				return
			}
			if err := deps.Invites.Create(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, req.Email, role, caps); err != nil {
				MapDomainError(w, r, err)
				return
			}
			// The email path has no id to report: Create predates this
			// response shape and returns none. It is the deprecated channel
			// and gains nothing from being reshaped -- the frontend reads
			// only `link`, and the list route supplies the row.
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{})
		case channelChoiceTelegram:
			if !scope.Flags.Enabled(domain.FlagTelegramSignIn) {
				MapDomainError(w, r, domain.ErrTelegramInvitesUnavailable)
				return
			}
			link, err := deps.Invites.CreateTelegram(r.Context(), scope.HouseholdID, scope.UserID,
				req.Name, role, caps)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			WriteJSON(w, http.StatusCreated, inviteCreatedDTO{ID: &link.ID, ExpiresAt: &link.ExpiresAt, Link: link.URL})
		default:
			// Unreachable while parseInviteChannelChoice is the only way
			// in, and present anyway: a choice added without a case here
			// refuses rather than falling through to the email path.
			MapDomainError(w, r, domain.ErrUnknownInviteChannel)
			return
		}
	}
}

// updateMemberRequest's fields are pointers for the same reason
// updateHouseholdRequest's and notificationPreferencesRequest's are (see
// household_handlers.go): a plain string/slice field can't tell "omitted"
// from "sent its zero value," so a capabilities-only request would blank
// Role to "" and fail as an unknown role, and vice versa. Don't go back to
// value fields here: it once shipped that way and forced the frontend to
// work around it by always sending both together.
type updateMemberRequest struct {
	Role         *string   `json:"role"`
	Capabilities *[]string `json:"capabilities"`
}

// handleUpdateMember sits behind requireOwner. On success the body echoes
// what was set; if usecase.ErrSessionRevocationFailed comes back after a
// successful update, the same body gets a warning field appended and stays
// 200, since the mutation did happen and reporting it as a failure would
// invite a pointless retry. This is checked before MapDomainError, which
// only ever sees the error, not the success body to append a warning to.
//
// This is a real PATCH: only fields present in the request change. The
// handler deliberately does NOT read the membership first to fill in
// omitted fields -- a read here, before the household lock, could carry a
// stale role into the write and silently undo a role change another owner
// made in between. Instead it hands a MembershipPatch to
// usecase.MemberService.Update, which fills omitted fields in itself from
// memberships read under the lock, and validates role and capabilities
// together against that locked, resolved state.
func handleUpdateMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		membershipID := chi.URLParam(r, "id")

		var req updateMemberRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		var patch usecase.MembershipPatch
		if req.Role != nil {
			role, err := domain.ParseRole(*req.Role)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			patch.Role = &role
		}
		if req.Capabilities != nil {
			caps, err := domain.ParseCapabilities(*req.Capabilities)
			if err != nil {
				MapDomainError(w, r, err)
				return
			}
			patch.Capabilities = &caps
		}

		written, err := deps.Members.Update(r.Context(), scope.HouseholdID, membershipID, patch)
		if err != nil && !errors.Is(err, usecase.ErrSessionRevocationFailed) {
			MapDomainError(w, r, err)
			return
		}
		body := map[string]any{"id": membershipID, "role": string(written.Role), "capabilities": written.Capabilities.Strings()}
		if err != nil {
			body["warning"] = sessionRevocationWarning
		}
		WriteJSON(w, http.StatusOK, body)
	}
}

// handleRemoveMember sits behind requireOwner. Its ordinary success is 204
// with no body; the one exception is the same ErrSessionRevocationFailed
// case handleUpdateMember has, which must carry a warning and so cannot be
// a bodyless 204 -- that case alone answers 200 with a small JSON body.
func handleRemoveMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := RequestScope(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in required.", nil)
			return
		}
		membershipID := chi.URLParam(r, "id")

		if err := deps.Members.Remove(r.Context(), scope.HouseholdID, membershipID); err != nil {
			if errors.Is(err, usecase.ErrSessionRevocationFailed) {
				WriteJSON(w, http.StatusOK, map[string]any{"status": "removed", "warning": sessionRevocationWarning})
				return
			}
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
