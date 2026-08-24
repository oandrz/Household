package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// openingBalanceLayout is the wire format for opening_balance_as_of: a
// calendar date, because "the balance was true on the 26th" is a fact about a
// day and not about an instant.
const openingBalanceLayout = "2006-01-02"

type moneyDTO struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

// accountDTO omits Balance, OpeningBalance and BalanceAsOf entirely for a
// limited member -- hence the pointers and omitempty rather than zero values.
// A zeroed amount still reads as a real balance, which is the failure this
// shape exists to make impossible.
//
// Balance and OpeningBalance are two different figures. Balance is what the
// account holds now: the opening balance plus every transaction dated on or
// after BalanceAsOf, summed in SQL (queries/account.sql). OpeningBalance is the
// figure someone asserted was true on BalanceAsOf, and BalanceAsOf is the day
// that assertion is about -- the two of them are one fact and are the pair a
// client edits and PATCHes back as openingBalanceMinor/openingBalanceAsOf.
// They were the same number until Transactions shipped, which is exactly why
// OpeningBalance has to be on the wire: a client that has only Balance to
// prefill an edit form from will write today's balance back as the opening
// one and move the household's net worth by every transaction since.
type accountDTO struct {
	ID                      string     `json:"id"`
	Nickname                string     `json:"nickname"`
	Type                    string     `json:"type"`
	OwnerMembershipID       *string    `json:"ownerMembershipId"`
	OwnerName               *string    `json:"ownerName"`
	Balance                 *moneyDTO  `json:"balance,omitempty"`
	OpeningBalance          *moneyDTO  `json:"openingBalance,omitempty"`
	BalanceAsOf             *string    `json:"balanceAsOf,omitempty"`
	CountTowardNetWorth     bool       `json:"countTowardNetWorth"`
	VisibleToLimitedMembers bool       `json:"visibleToLimitedMembers"`
	ArchivedAt              *time.Time `json:"archivedAt"`
}

type breakdownDTO struct {
	Type       string `json:"type"`
	TotalMinor int64  `json:"totalMinor"`
}

type excludedDTO struct {
	AccountID string `json:"accountId"`
	Currency  string `json:"currency"`
}

// trendPointDTO is one bar. NetWorthMinor is a pointer WITHOUT omitempty, so
// an unknown month arrives as an explicit null rather than a missing key: the
// chart needs the slot to keep its axis aligned, and a zero would be a claim
// about the household's money that nobody can make.
type trendPointDTO struct {
	Month         string `json:"month"`
	NetWorthMinor *int64 `json:"netWorthMinor"`
	Complete      bool   `json:"complete"`
}

// trendDTO carries the change as integer basis points -- 210 is 2.10%. A
// percentage is not money, so the int64-minor-units rule does not literally
// apply, but there is no reason to put a float on this wire either, and
// omitempty is wrong for it too: the field is absent when suppressed, and 0
// is a real reading meaning "unchanged".
type trendDTO struct {
	Points            []trendPointDTO `json:"points"`
	ChangeBasisPoints *int64          `json:"changeBasisPoints,omitempty"`
}

// summaryDTO's NetWorthMinor, AssetsMinor and LiabilitiesMinor are pointers so
// that an incomputable summary carries no figures at all rather than zeros.
// Zero is a claim about the household's money; the truth in that state is that
// we cannot compute it.
type summaryDTO struct {
	Currency         string         `json:"currency"`
	Computable       bool           `json:"computable"`
	NetWorthMinor    *int64         `json:"netWorthMinor,omitempty"`
	AssetsMinor      *int64         `json:"assetsMinor,omitempty"`
	LiabilitiesMinor *int64         `json:"liabilitiesMinor,omitempty"`
	Breakdown        []breakdownDTO `json:"breakdown"`
	ExcludedNoRate   []excludedDTO  `json:"excludedNoRate"`
	ExcludedByChoice int            `json:"excludedByChoice"`
	Trend            *trendDTO      `json:"trend,omitempty"`
}

type accountsResponse struct {
	Accounts []accountDTO `json:"accounts"`
	Summary  *summaryDTO  `json:"summary,omitempty"`
}

// handleListAccounts is the one endpoint the Finances screen reads. It returns
// the list and the summary together because they are one screen and must
// describe the same set of rows -- two endpoints would mean writing the
// redaction below twice, and a rule written twice is a rule fixed once.
func handleListAccounts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Every account route sits behind requireCapability (router.go), which
		// already calls RequestScope and answers 403 for a caller it comes back
		// false for -- so by the time any of the five handlers in this file
		// runs, ok is guaranteed true and the explicit check other handlers make
		// (e.g. handleGetHousehold) would be dead code here.
		scope, _ := RequestScope(r)
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		views, err := deps.Accounts.List(r.Context(), scope.HouseholdID, includeArchived)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		// The redaction is here, in the handler, not in AccountService: it is a
		// rule about who is asking, and services in this codebase never take an
		// actor.
		//
		// The condition names the role that may see everything, rather than the
		// role that may not. Those are the same test while `owner` and `limited`
		// are the only roles, and they stop being the same the day a third one
		// arrives -- the "adult who is not an owner" this product will plausibly
		// want. Written the other way round, that new role would silently
		// receive every balance and the net worth, and no test in the suite
		// would go red. Role comes from a database column, and this codebase
		// fails closed on values it did not construct.
		if scope.Membership.Role != domain.RoleOwner {
			WriteJSON(w, http.StatusOK, accountsResponse{Accounts: redactedAccounts(views)})
			return
		}

		summary, err := deps.Accounts.Summary(r.Context(), scope.HouseholdID, views, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}

		out := accountsResponse{Accounts: make([]accountDTO, 0, len(views))}
		for _, v := range views {
			out.Accounts = append(out.Accounts, toAccountDTO(v))
		}
		dto := toSummaryDTO(summary)
		out.Summary = &dto
		WriteJSON(w, http.StatusOK, out)
	}
}

// redactedAccounts is what a limited member sees: only the accounts shared
// with them, and no amounts anywhere. The summary is omitted by the caller.
func redactedAccounts(views []usecase.AccountView) []accountDTO {
	out := make([]accountDTO, 0, len(views))
	for _, v := range views {
		if !v.Account.VisibleToLimitedMembers {
			continue
		}
		dto := toAccountDTO(v)
		dto.Balance = nil
		// The opening balance is an amount too, and one that is close enough
		// to the current one to be just as revealing. A limited member who may
		// not see what an account holds may not see what it started with.
		dto.OpeningBalance = nil
		dto.BalanceAsOf = nil
		out = append(out, dto)
	}
	return out
}

func toAccountDTO(v usecase.AccountView) accountDTO {
	dto := accountDTO{
		ID:                      v.Account.ID,
		Nickname:                v.Account.Nickname,
		Type:                    string(v.Account.Type),
		CountTowardNetWorth:     v.Account.CountTowardNetWorth,
		VisibleToLimitedMembers: v.Account.VisibleToLimitedMembers,
		ArchivedAt:              v.Account.ArchivedAt,
		Balance:                 &moneyDTO{AmountMinor: v.Balance.Amount, Currency: v.Balance.Currency},
		OpeningBalance: &moneyDTO{
			AmountMinor: v.Account.OpeningBalance.Amount,
			Currency:    v.Account.OpeningBalance.Currency,
		},
	}
	// "" means shared, and the wire form of shared is JSON null -- not an
	// empty string, which would read as a member whose id happens to be blank.
	if v.Account.OwnerMembershipID != "" {
		id, name := v.Account.OwnerMembershipID, v.OwnerName
		dto.OwnerMembershipID, dto.OwnerName = &id, &name
	}
	asOf := v.Account.OpeningBalanceAsOf.Format(openingBalanceLayout)
	dto.BalanceAsOf = &asOf
	return dto
}

func toSummaryDTO(s usecase.NetWorthSummary) summaryDTO {
	dto := summaryDTO{
		Currency:         s.Currency,
		Computable:       s.Computable,
		Breakdown:        make([]breakdownDTO, 0, len(s.Breakdown)),
		ExcludedNoRate:   make([]excludedDTO, 0, len(s.ExcludedNoRate)),
		ExcludedByChoice: s.ExcludedByChoice,
	}
	// The three figures are attached only when they mean something. An
	// incomputable summary carries none of them, so the frontend cannot render
	// a zero it was never given.
	if s.Computable {
		netWorth, assets, liabilities := s.NetWorth.Amount, s.Assets.Amount, s.Liabilities.Amount
		dto.NetWorthMinor, dto.AssetsMinor, dto.LiabilitiesMinor = &netWorth, &assets, &liabilities
	}
	if s.Computable && s.Trend != nil {
		points := make([]trendPointDTO, 0, len(s.Trend.Points))
		for _, p := range s.Trend.Points {
			point := trendPointDTO{Month: p.Month.Format(monthLayout), Complete: p.Complete}
			if p.NetWorth != nil {
				amount := p.NetWorth.Amount
				point.NetWorthMinor = &amount
			}
			points = append(points, point)
		}
		dto.Trend = &trendDTO{Points: points, ChangeBasisPoints: s.Trend.ChangeBasisPoints}
	}
	for _, entry := range s.Breakdown {
		dto.Breakdown = append(dto.Breakdown, breakdownDTO{
			Type: string(entry.Type), TotalMinor: entry.Total.Amount,
		})
	}
	for _, ex := range s.ExcludedNoRate {
		dto.ExcludedNoRate = append(dto.ExcludedNoRate, excludedDTO{
			AccountID: ex.AccountID, Currency: ex.Currency,
		})
	}
	return dto
}

type createAccountRequest struct {
	Nickname                string  `json:"nickname"`
	Type                    string  `json:"type"`
	OwnerMembershipID       *string `json:"ownerMembershipId"`
	OpeningBalanceMinor     int64   `json:"openingBalanceMinor"`
	OpeningBalanceCurrency  string  `json:"openingBalanceCurrency"`
	OpeningBalanceAsOf      string  `json:"openingBalanceAsOf"`
	CountTowardNetWorth     *bool   `json:"countTowardNetWorth"`
	VisibleToLimitedMembers *bool   `json:"visibleToLimitedMembers"`
}

// updateAccountRequest's fields are all pointers so a field the caller did not
// name reaches usecase.AccountUpdate as nil and keeps its stored value --
// the same real-patch convention TestUpdateHouseholdIsARealPatch pins for
// PATCH /household.
type updateAccountRequest struct {
	Nickname                *string `json:"nickname"`
	Type                    *string `json:"type"`
	OwnerMembershipID       *string `json:"ownerMembershipId"`
	OpeningBalanceMinor     *int64  `json:"openingBalanceMinor"`
	OpeningBalanceCurrency  *string `json:"openingBalanceCurrency"`
	OpeningBalanceAsOf      *string `json:"openingBalanceAsOf"`
	CountTowardNetWorth     *bool   `json:"countTowardNetWorth"`
	VisibleToLimitedMembers *bool   `json:"visibleToLimitedMembers"`
}

func handleCreateAccount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createAccountRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		asOf, err := time.Parse(openingBalanceLayout, req.OpeningBalanceAsOf)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_AS_OF",
				"That date could not be read. Use YYYY-MM-DD.", nil)
			return
		}

		in := usecase.NewAccount{
			HouseholdID:            scope.HouseholdID,
			Nickname:               req.Nickname,
			Type:                   req.Type,
			OpeningBalanceMinor:    req.OpeningBalanceMinor,
			OpeningBalanceCurrency: req.OpeningBalanceCurrency,
			OpeningBalanceAsOf:     asOf,
			// The design draws these toggles on and off respectively, and an
			// omitted field must land on the same default the form shows --
			// otherwise a client that sends neither gets an account that
			// counts toward nothing and is visible to children.
			CountTowardNetWorth:     true,
			VisibleToLimitedMembers: false,
		}
		if req.OwnerMembershipID != nil {
			in.OwnerMembershipID = *req.OwnerMembershipID
		}
		if req.CountTowardNetWorth != nil {
			in.CountTowardNetWorth = *req.CountTowardNetWorth
		}
		if req.VisibleToLimitedMembers != nil {
			in.VisibleToLimitedMembers = *req.VisibleToLimitedMembers
		}

		created, err := deps.Accounts.Create(r.Context(), in)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, created.ID, http.StatusCreated)
	}
}

func handleUpdateAccount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req updateAccountRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		patch := usecase.AccountUpdate{
			Nickname:                req.Nickname,
			Type:                    req.Type,
			OwnerMembershipID:       req.OwnerMembershipID,
			OpeningBalanceMinor:     req.OpeningBalanceMinor,
			OpeningBalanceCurrency:  req.OpeningBalanceCurrency,
			CountTowardNetWorth:     req.CountTowardNetWorth,
			VisibleToLimitedMembers: req.VisibleToLimitedMembers,
		}
		if req.OpeningBalanceAsOf != nil {
			asOf, err := time.Parse(openingBalanceLayout, *req.OpeningBalanceAsOf)
			if err != nil {
				WriteError(w, http.StatusUnprocessableEntity, "INVALID_AS_OF",
					"That date could not be read. Use YYYY-MM-DD.", nil)
				return
			}
			patch.OpeningBalanceAsOf = &asOf
		}

		id := chi.URLParam(r, "id")
		if _, err := deps.Accounts.Update(r.Context(), scope.HouseholdID, id, patch); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

func handleArchiveAccount(deps Deps) http.HandlerFunc { return setArchived(deps, true) }
func handleRestoreAccount(deps Deps) http.HandlerFunc { return setArchived(deps, false) }

// setArchived backs both the archive and the restore route. One function
// rather than two near-identical ones: the pair differ by a single boolean,
// and this project's repeated lesson is that a rule written twice is a rule
// fixed once.
func setArchived(deps Deps, archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		id := chi.URLParam(r, "id")
		if _, err := deps.Accounts.SetArchived(r.Context(), scope.HouseholdID, id, archived); err != nil {
			MapDomainError(w, r, err)
			return
		}
		writeAccount(w, r, deps, scope.HouseholdID, id, http.StatusOK)
	}
}

// writeAccount re-reads the account through Get so every mutating response
// carries the owner's display name, which the write queries do not return. It
// always answers with a body, never 204: apiFetch throws INVALID_RESPONSE on
// an ok response it cannot parse.
//
// status is a parameter, not a constant, because the four callers do not
// agree on it: create answers 201, matching this API's own convention for
// POST /spaces and POST /household/members/invite, while update, archive and
// restore all answer 200 -- they are edits to a row that already existed, not
// the creation of one. The four used to share one status only because they
// shared this function; once a caller needed to differ, giving the shared
// code a parameter was cheaper than duplicating it just to vary that.
func writeAccount(w http.ResponseWriter, r *http.Request, deps Deps, householdID, accountID string, status int) {
	view, err := deps.Accounts.Get(r.Context(), householdID, accountID)
	if err != nil {
		MapDomainError(w, r, err)
		return
	}
	WriteJSON(w, status, toAccountDTO(view))
}
