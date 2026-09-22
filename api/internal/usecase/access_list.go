package usecase

import (
	"context"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// The household access list (partner invite lobby, milestone 3): every live
// way into a household that is not a password -- API tokens and linked
// Telegram chats. docs/superpowers/specs/2026-09-23-hearth-household-access-list-design.md.
//
// This service takes no actor. Whether the caller sees the whole household
// (an owner) or only their own rows (a limited member) is decided by the
// HTTP handler, which picks ForHousehold or ForMember (ADR 8).

// HouseholdTokenLister returns a household's live API tokens -- not revoked,
// not expired -- newest first, and never another household's. Empty is an
// empty slice, not domain.ErrNotFound. Its own narrow port rather than a
// method on APITokenRepository, so the doubles of that wider port do not
// have to grow a method they never use.
type HouseholdTokenLister interface {
	ListForHousehold(ctx context.Context, householdID string) ([]domain.APIToken, error)
}

// HouseholdChatLister returns every Telegram chat bound to a member of the
// household, most recently linked first. Empty is an empty slice.
type HouseholdChatLister interface {
	ListForHousehold(ctx context.Context, householdID string) ([]TelegramBinding, error)
}

// MemberLister is the one MembershipRepository method this service needs:
// the display name to label each row with.
type MemberLister interface {
	List(ctx context.Context, householdID string) ([]MemberView, error)
}

type AccessListDeps struct {
	Tokens  HouseholdTokenLister
	Chats   HouseholdChatLister
	Members MemberLister
}

type AccessListService struct{ d AccessListDeps }

func NewAccessListService(d AccessListDeps) *AccessListService { return &AccessListService{d: d} }

// AccessToken is one token row, labelled with whose it is.
type AccessToken struct {
	Token      domain.APIToken
	MemberName string
}

// AccessChat is one linked chat, labelled with whose it is. It carries no
// chat id: nothing on the access list needs one, so it stops here.
type AccessChat struct {
	UserID       string
	MemberName   string
	ChatUsername string
	LinkedAt     time.Time
}

// AccessList is the whole answer. Both slices are non-nil, so the HTTP
// layer sends [] rather than null.
type AccessList struct {
	Tokens []AccessToken
	Chats  []AccessChat
}

// ForHousehold is every member's rows. withChats false skips the chat query
// entirely -- the caller passes false when Telegram is off for this
// household or no bot is configured.
func (s *AccessListService) ForHousehold(ctx context.Context, householdID string, withChats bool) (AccessList, error) {
	return s.list(ctx, householdID, "", withChats)
}

// ForMember is ForHousehold narrowed to one member's own rows. It filters
// the same household query rather than running a second one, so both paths
// share one query and one set of repository tests.
func (s *AccessListService) ForMember(ctx context.Context, householdID, userID string, withChats bool) (AccessList, error) {
	return s.list(ctx, householdID, userID, withChats)
}

// list does the work. onlyUserID "" means everyone.
func (s *AccessListService) list(ctx context.Context, householdID, onlyUserID string, withChats bool) (AccessList, error) {
	members, err := s.d.Members.List(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	names := make(map[string]string, len(members))
	for _, m := range members {
		names[m.User.ID] = m.User.DisplayName
	}
	// A row whose user is not a current member is dropped: there is no name
	// to show and no reason for it to be here (fail closed).
	include := func(userID string) (string, bool) {
		name, isMember := names[userID]
		if !isMember {
			return "", false
		}
		if onlyUserID != "" && userID != onlyUserID {
			return "", false
		}
		return name, true
	}

	tokens, err := s.d.Tokens.ListForHousehold(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	out := AccessList{Tokens: []AccessToken{}, Chats: []AccessChat{}}
	for _, tok := range tokens {
		if name, ok := include(tok.UserID); ok {
			out.Tokens = append(out.Tokens, AccessToken{Token: tok, MemberName: name})
		}
	}

	if !withChats {
		return out, nil
	}
	chats, err := s.d.Chats.ListForHousehold(ctx, householdID)
	if err != nil {
		return AccessList{}, err
	}
	for _, c := range chats {
		if name, ok := include(c.UserID); ok {
			out.Chats = append(out.Chats, AccessChat{
				UserID: c.UserID, MemberName: name, ChatUsername: c.ChatUsername, LinkedAt: c.LinkedAt,
			})
		}
	}
	return out, nil
}
