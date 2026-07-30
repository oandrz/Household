package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type AccountRepo struct{ q *sqlcgen.Queries }

func NewAccountRepo(db *DB) *AccountRepo {
	return &AccountRepo{q: sqlcgen.New(db.Pool())}
}

// var _ pins AccountRepo to usecase.AccountLookup at compile time, the
// narrower port TransactionService depends on -- the same reason
// category_repo.go pins CategoryRepo to CategoryLookup.
var _ usecase.AccountLookup = (*AccountRepo)(nil)

func (r *AccountRepo) List(ctx context.Context, householdID string, includeArchived bool) ([]usecase.AccountView, error) {
	if includeArchived {
		rows, err := r.q.ListAccountsIncludingArchived(ctx, uuid(householdID))
		if err != nil {
			return nil, translate(err, "list accounts including archived")
		}
		out := make([]usecase.AccountView, 0, len(rows))
		for _, row := range rows {
			out = append(out, toAccountViewIncludingArchived(row))
		}
		return out, nil
	}

	rows, err := r.q.ListAccounts(ctx, uuid(householdID))
	if err != nil {
		return nil, translate(err, "list accounts")
	}
	out := make([]usecase.AccountView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccountView(row))
	}
	return out, nil
}

func (r *AccountRepo) Get(ctx context.Context, householdID, accountID string) (usecase.AccountView, error) {
	row, err := r.q.GetAccount(ctx, sqlcgen.GetAccountParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(accountID),
	})
	if err != nil {
		return usecase.AccountView{}, translate(err, "get account")
	}
	return toAccountViewFromGet(row), nil
}

func (r *AccountRepo) Create(ctx context.Context, a domain.Account) (domain.Account, error) {
	row, err := r.q.CreateAccount(ctx, sqlcgen.CreateAccountParams{
		HouseholdID:             uuid(a.HouseholdID),
		Nickname:                a.Nickname,
		Type:                    string(a.Type),
		OwnerMembershipID:       nullableUUID(optionalID(a.OwnerMembershipID)),
		OpeningBalanceMinor:     a.OpeningBalance.Amount,
		OpeningBalanceCurrency:  a.OpeningBalance.Currency,
		OpeningBalanceAsOf:      dateOnly(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
	})
	if err != nil {
		return domain.Account{}, translate(err, "create account")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) Update(ctx context.Context, a domain.Account) (domain.Account, error) {
	row, err := r.q.UpdateAccount(ctx, sqlcgen.UpdateAccountParams{
		HouseholdID:             uuid(a.HouseholdID),
		ID:                      uuid(a.ID),
		Nickname:                a.Nickname,
		Type:                    string(a.Type),
		OwnerMembershipID:       nullableUUID(optionalID(a.OwnerMembershipID)),
		OpeningBalanceMinor:     a.OpeningBalance.Amount,
		OpeningBalanceCurrency:  a.OpeningBalance.Currency,
		OpeningBalanceAsOf:      dateOnly(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
	})
	if err != nil {
		return domain.Account{}, translate(err, "update account")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) SetArchived(ctx context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error) {
	stamp := pgtype.Timestamptz{}
	if archived {
		stamp = timestamptz(at)
	}
	row, err := r.q.SetAccountArchived(ctx, sqlcgen.SetAccountArchivedParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(accountID),
		ArchivedAt:  stamp,
	})
	if err != nil {
		return domain.Account{}, translate(err, "set account archived")
	}
	return toAccount(row), nil
}

func (r *AccountRepo) MembershipBelongsToHousehold(ctx context.Context, householdID, membershipID string) (bool, error) {
	ok, err := r.q.MembershipBelongsToHousehold(ctx, sqlcgen.MembershipBelongsToHouseholdParams{
		ID:          uuid(membershipID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return false, translate(err, "check membership household")
	}
	return ok, nil
}

// optionalID turns the "" <-> NULL convention into the *string nullableUUID
// expects. It is the account owner's half of the same rule nullableText
// implements for users.email and users.password_hash.
func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// toAccount maps the columns every account query returns into the domain
// type. sqlc generates a distinct row struct per query (ListAccountsRow,
// ListAccountsIncludingArchivedRow, GetAccountRow, and the plain Account for
// the RETURNING queries) and flattens each one's `a.*` columns directly onto
// the row rather than embedding sqlcgen.Account, so the three view converters
// below rebuild an Account from their row's fields before calling this --
// the mapping from Account to domain.Account itself exists once, here.
func toAccount(a sqlcgen.Account) domain.Account {
	return domain.Account{
		ID:                      uuidToString(a.ID),
		HouseholdID:             uuidToString(a.HouseholdID),
		Nickname:                a.Nickname,
		Type:                    domain.AccountType(a.Type),
		OwnerMembershipID:       optionalIDToString(a.OwnerMembershipID),
		OpeningBalance:          domain.Money{Amount: a.OpeningBalanceMinor, Currency: a.OpeningBalanceCurrency},
		OpeningBalanceAsOf:      dateToTime(a.OpeningBalanceAsOf),
		CountTowardNetWorth:     a.CountTowardNetWorth,
		VisibleToLimitedMembers: a.VisibleToLimitedMembers,
		ArchivedAt:              timePtrOf(a.ArchivedAt),
	}
}

// optionalIDToString is optionalID's inverse: a NULL owner_membership_id
// comes back as "", meaning shared -- never as the zero uuid, which would
// read as a real membership that happens not to exist.
func optionalIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

// buildView is where AccountView.Balance is decided. It is the opening
// balance plus every transaction dated on or after opening_balance_as_of,
// summed in SQL -- see the balance_minor column in queries/account.sql for
// why the comparison is >= (start-of-day rule) and why the incoming side
// prefers received_amount_minor.
//
// The currency is the account's own. Every transaction on an account is
// denominated in that account's currency, so nothing here converts, and a
// mixed-currency household's accounts each stay in their own unit until
// AccountService.Summary converts them.
func buildView(a domain.Account, ownerName *string, balanceMinor int64) usecase.AccountView {
	return usecase.AccountView{
		Account:   a,
		OwnerName: stringOrEmpty(ownerName),
		Balance:   domain.Money{Amount: balanceMinor, Currency: a.OpeningBalance.Currency},
	}
}

func toAccountView(row sqlcgen.ListAccountsRow) usecase.AccountView {
	return buildView(toAccount(sqlcgen.Account{
		ID:                      row.ID,
		HouseholdID:             row.HouseholdID,
		Nickname:                row.Nickname,
		Type:                    row.Type,
		OwnerMembershipID:       row.OwnerMembershipID,
		OpeningBalanceMinor:     row.OpeningBalanceMinor,
		OpeningBalanceCurrency:  row.OpeningBalanceCurrency,
		OpeningBalanceAsOf:      row.OpeningBalanceAsOf,
		CountTowardNetWorth:     row.CountTowardNetWorth,
		VisibleToLimitedMembers: row.VisibleToLimitedMembers,
		ArchivedAt:              row.ArchivedAt,
		CreatedAt:               row.CreatedAt,
	}), row.OwnerName, row.BalanceMinor)
}

func toAccountViewIncludingArchived(row sqlcgen.ListAccountsIncludingArchivedRow) usecase.AccountView {
	return buildView(toAccount(sqlcgen.Account{
		ID:                      row.ID,
		HouseholdID:             row.HouseholdID,
		Nickname:                row.Nickname,
		Type:                    row.Type,
		OwnerMembershipID:       row.OwnerMembershipID,
		OpeningBalanceMinor:     row.OpeningBalanceMinor,
		OpeningBalanceCurrency:  row.OpeningBalanceCurrency,
		OpeningBalanceAsOf:      row.OpeningBalanceAsOf,
		CountTowardNetWorth:     row.CountTowardNetWorth,
		VisibleToLimitedMembers: row.VisibleToLimitedMembers,
		ArchivedAt:              row.ArchivedAt,
		CreatedAt:               row.CreatedAt,
	}), row.OwnerName, row.BalanceMinor)
}

func toAccountViewFromGet(row sqlcgen.GetAccountRow) usecase.AccountView {
	return buildView(toAccount(sqlcgen.Account{
		ID:                      row.ID,
		HouseholdID:             row.HouseholdID,
		Nickname:                row.Nickname,
		Type:                    row.Type,
		OwnerMembershipID:       row.OwnerMembershipID,
		OpeningBalanceMinor:     row.OpeningBalanceMinor,
		OpeningBalanceCurrency:  row.OpeningBalanceCurrency,
		OpeningBalanceAsOf:      row.OpeningBalanceAsOf,
		CountTowardNetWorth:     row.CountTowardNetWorth,
		VisibleToLimitedMembers: row.VisibleToLimitedMembers,
		ArchivedAt:              row.ArchivedAt,
		CreatedAt:               row.CreatedAt,
	}), row.OwnerName, row.BalanceMinor)
}
