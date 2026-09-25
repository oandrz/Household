package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// The three ports are one method each, so each double is a func.

type tokenListerFunc func(ctx context.Context, householdID string) ([]domain.APIToken, error)

func (f tokenListerFunc) ListForHousehold(ctx context.Context, h string) ([]domain.APIToken, error) {
	return f(ctx, h)
}

type chatListerFunc func(ctx context.Context, householdID string) ([]usecase.TelegramBinding, error)

func (f chatListerFunc) ListForHousehold(ctx context.Context, h string) ([]usecase.TelegramBinding, error) {
	return f(ctx, h)
}

type memberListerFunc func(ctx context.Context, householdID string) ([]usecase.MemberView, error)

func (f memberListerFunc) List(ctx context.Context, h string) ([]usecase.MemberView, error) {
	return f(ctx, h)
}

func accessMember(userID, name string) usecase.MemberView {
	return usecase.MemberView{
		Membership: domain.Membership{UserID: userID, HouseholdID: "h1"},
		User:       domain.User{ID: userID, DisplayName: name},
	}
}

// accessService builds the service over a household of Alex and Sam, each
// with one token and one chat, plus a token belonging to someone who is no
// longer a member.
func accessService(t *testing.T, chatsCalled *bool) *usecase.AccessListService {
	t.Helper()
	linked := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return usecase.NewAccessListService(usecase.AccessListDeps{
		Members: memberListerFunc(func(context.Context, string) ([]usecase.MemberView, error) {
			return []usecase.MemberView{accessMember("u-alex", "Alex"), accessMember("u-sam", "Sam")}, nil
		}),
		Tokens: tokenListerFunc(func(context.Context, string) ([]domain.APIToken, error) {
			return []domain.APIToken{
				{ID: "t-sam", UserID: "u-sam", Name: "sam script"},
				{ID: "t-alex", UserID: "u-alex", Name: "alex laptop"},
				{ID: "t-gone", UserID: "u-gone", Name: "left behind"},
			}, nil
		}),
		Chats: chatListerFunc(func(context.Context, string) ([]usecase.TelegramBinding, error) {
			if chatsCalled != nil {
				*chatsCalled = true
			}
			return []usecase.TelegramBinding{
				{UserID: "u-sam", ChatID: 2, ChatUsername: "sam_k", LinkedAt: linked},
				{UserID: "u-alex", ChatID: 1, LinkedAt: linked},
			}, nil
		}),
	})
}

func TestForHouseholdListsEveryMembersTokensAndChatsWithTheirNames(t *testing.T) {
	got, err := accessService(t, nil).ForHousehold(context.Background(), "h1", true)
	if err != nil {
		t.Fatalf("ForHousehold() = %v", err)
	}
	if len(got.Tokens) != 2 || got.Tokens[0].MemberName != "Sam" || got.Tokens[1].MemberName != "Alex" {
		t.Fatalf("tokens = %+v, want Sam's then Alex's, order kept", got.Tokens)
	}
	if len(got.Chats) != 2 || got.Chats[0].MemberName != "Sam" || got.Chats[0].ChatUsername != "sam_k" {
		t.Fatalf("chats = %+v", got.Chats)
	}
}

// A token whose user is no longer a member has no name to show and no
// business being on the list. Dropping it is the fail-closed choice.
func TestForHouseholdDropsRowsWhoseUserIsNotACurrentMember(t *testing.T) {
	got, _ := accessService(t, nil).ForHousehold(context.Background(), "h1", true)
	for _, tok := range got.Tokens {
		if tok.Token.ID == "t-gone" {
			t.Fatal("a non-member's token is listed")
		}
	}
}

func TestForMemberReturnsOnlyThatMembersTokensAndChat(t *testing.T) {
	got, err := accessService(t, nil).ForMember(context.Background(), "h1", "u-alex", true)
	if err != nil {
		t.Fatalf("ForMember() = %v", err)
	}
	if len(got.Tokens) != 1 || got.Tokens[0].Token.ID != "t-alex" {
		t.Fatalf("tokens = %+v, want Alex's only", got.Tokens)
	}
	if len(got.Chats) != 1 || got.Chats[0].UserID != "u-alex" {
		t.Fatalf("chats = %+v, want Alex's only", got.Chats)
	}
}

func TestWithoutChatsTheChatListerIsNeverAsked(t *testing.T) {
	called := false
	got, err := accessService(t, &called).ForHousehold(context.Background(), "h1", false)
	if err != nil {
		t.Fatalf("ForHousehold() = %v", err)
	}
	if called {
		t.Fatal("the chat lister was asked although chats are off")
	}
	if got.Chats == nil || len(got.Chats) != 0 {
		t.Fatalf("chats = %#v, want an empty non-nil slice", got.Chats)
	}
}

func TestAnAccessListRepositoryFailureIsReturnedNotSwallowed(t *testing.T) {
	boom := errors.New("boom")
	svc := usecase.NewAccessListService(usecase.AccessListDeps{
		Members: memberListerFunc(func(context.Context, string) ([]usecase.MemberView, error) { return nil, nil }),
		Tokens:  tokenListerFunc(func(context.Context, string) ([]domain.APIToken, error) { return nil, boom }),
		Chats:   chatListerFunc(func(context.Context, string) ([]usecase.TelegramBinding, error) { return nil, nil }),
	})
	if _, err := svc.ForHousehold(context.Background(), "h1", true); !errors.Is(err, boom) {
		t.Fatalf("ForHousehold() error = %v, want boom", err)
	}
}
