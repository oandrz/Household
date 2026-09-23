package httpadapter

import (
	"net/http"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The household access list: every live API token and linked Telegram chat,
// each labelled with its member. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
// Revoking reuses DELETE /auth/tokens/{id} and DELETE /auth/telegram, both
// already scoped to the caller's own rows.

type accessTokenDTO struct {
	ID         string     `json:"id"`
	MemberID   string     `json:"memberId"`
	MemberName string     `json:"memberName"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// accessChatDTO has no chat id on purpose (spec decision 9).
type accessChatDTO struct {
	MemberID     string    `json:"memberId"`
	MemberName   string    `json:"memberName"`
	ChatUsername string    `json:"chatUsername"`
	LinkedAt     time.Time `json:"linkedAt"`
}

type accessResponse struct {
	TelegramEnabled bool             `json:"telegramEnabled"`
	Tokens          []accessTokenDTO `json:"tokens"`
	Chats           []accessChatDTO  `json:"chats"`
}

func handleHouseholdAccess(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		// Chats only when this household has Telegram on AND a bot exists --
		// the same two conditions that decide whether the Telegram routes
		// answer at all (requireFeature, and handleTelegramBinding's nil check).
		withChats := deps.TelegramLink != nil && scope.Flags.Enabled(domain.FlagTelegramSignIn)

		// Who sees what is decided here, not in the service (ADR 8). The
		// switch refuses any role it does not know: a new role must be given
		// a rule here before it can see anything.
		var list usecase.AccessList
		var err error
		switch scope.Membership.Role {
		case domain.RoleOwner:
			list, err = deps.Access.ForHousehold(r.Context(), scope.HouseholdID, withChats)
		case domain.RoleLimited:
			list, err = deps.Access.ForMember(r.Context(), scope.HouseholdID, scope.UserID, withChats)
		default:
			WriteError(w, http.StatusForbidden, "FORBIDDEN", "You can't see this household's access list.", nil)
			return
		}
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := accessResponse{
			TelegramEnabled: withChats,
			Tokens:          make([]accessTokenDTO, 0, len(list.Tokens)),
			Chats:           make([]accessChatDTO, 0, len(list.Chats)),
		}
		for _, row := range list.Tokens {
			t := row.Token
			out.Tokens = append(out.Tokens, accessTokenDTO{
				ID: t.ID, MemberID: t.UserID, MemberName: row.MemberName, Name: t.Name, Prefix: t.Prefix,
				CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt,
			})
		}
		for _, c := range list.Chats {
			out.Chats = append(out.Chats, accessChatDTO{
				MemberID: c.UserID, MemberName: c.MemberName, ChatUsername: c.ChatUsername, LinkedAt: c.LinkedAt,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}
