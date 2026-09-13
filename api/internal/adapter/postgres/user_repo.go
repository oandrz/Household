package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// UserRepo keeps the pool alongside the pool-backed *sqlcgen.Queries, just as
// InviteRepo does, because CreateWithMembership needs to begin its own
// transaction -- something a *sqlcgen.Queries built once at construction
// time cannot do on its own.
type UserRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()} }

func (r *UserRepo) ByEmail(ctx context.Context, email string) (usecase.StoredUser, error) {
	row, err := r.q.GetUserByEmail(ctx, text(email))
	if err != nil {
		return usecase.StoredUser{}, translate(err, "get user by email")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial), nil
}

func (r *UserRepo) ByID(ctx context.Context, id string) (usecase.StoredUser, error) {
	row, err := r.q.GetUserByID(ctx, uuid(id))
	if err != nil {
		return usecase.StoredUser{}, translate(err, "get user by id")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial), nil
}

func (r *UserRepo) Create(ctx context.Context, email, passwordHash, displayName string) (domain.User, error) {
	row, err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   displayName,
		AvatarInitial: initialOf(displayName),
	})
	if err != nil {
		return domain.User{}, translate(err, "create user")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial).User, nil
}

// CreateWithMembership creates the user and their membership in one
// transaction, mirroring the pattern InviteRepo.Accept already establishes:
// begin a transaction from the pool, bind sqlcgen.Queries to it with WithTx,
// run both statements, commit -- with a deferred rollback that is a no-op
// once Commit has succeeded.
//
// Either both writes happen or neither does. Create's child branch (a
// limited member with no email of their own) used to call Create and then
// Members.Create as two independent statements: if the second failed, the
// first had already committed, leaving an orphaned user with a NULL email
// and no membership. Because that email is NULL, it is not
// unique-constrained the way a real email would be, so a retry would not
// fail loudly -- it would silently create another orphan, and another, each
// time the caller retried.
func (r *UserRepo) CreateWithMembership(ctx context.Context, email, passwordHash, displayName string,
	m domain.Membership) (domain.User, domain.Membership, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, domain.Membership{}, fmt.Errorf("begin create-with-membership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   displayName,
		AvatarInitial: initialOf(displayName),
	})
	if err != nil {
		return domain.User{}, domain.Membership{}, translate(err, "create user")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  uuid(m.HouseholdID),
		UserID:       userRow.ID,
		Role:         string(m.Role),
		Capabilities: m.Capabilities.Strings(),
	})
	if err != nil {
		return domain.User{}, domain.Membership{}, translate(err, "create membership")
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, domain.Membership{}, fmt.Errorf("commit create-with-membership transaction: %w", err)
	}

	user := toStoredUser(userRow.ID, userRow.Email, userRow.PasswordHash, userRow.DisplayName, userRow.AvatarInitial).User
	membership, err := toMembership(membershipRow.ID, membershipRow.HouseholdID, membershipRow.UserID,
		membershipRow.Role, membershipRow.Capabilities)
	if err != nil {
		return domain.User{}, domain.Membership{}, err
	}
	return user, membership, nil
}

func (r *UserRepo) FindOrphanedChild(ctx context.Context, displayName string) (domain.User, error) {
	row, err := r.q.GetOrphanedCredentiallessUserByName(ctx, displayName)
	if err != nil {
		return domain.User{}, translate(err, "find orphaned child")
	}
	return toStoredUser(row.ID, row.Email, row.PasswordHash, row.DisplayName, row.AvatarInitial).User, nil
}

func (r *UserRepo) SetPasswordHash(ctx context.Context, userID, hash string) error {
	return translate(r.q.SetPasswordHash(ctx, sqlcgen.SetPasswordHashParams{
		ID: uuid(userID), PasswordHash: nullableText(hash),
	}), "set password hash")
}

// initialOf derives the avatar initial from a display name.
//
// It takes the first *rune*, not the first byte: the old name[:1] byte slice
// took one byte of what may be a multi-byte UTF-8 sequence, so every non-ASCII
// name got an invalid fragment that rendered as the replacement character --
// permanently, since there is no profile-edit endpoint to correct it.
//
// It uses cases.Upper(language.Und) rather than strings.ToUpper, because the
// standard library applies simple case mapping only: 'ß' does not uppercase
// at all. Full case mapping is what a user-supplied name from an unknown script
// deserves. language.Und (undefined/root) is used because the locale is
// genuinely unknown at initial creation time. The result can be more than one
// character: cases.Upper(language.Und).String("ß") is "SS". users.avatar_initial
// is text (migration 00003) rather than char(1) for exactly this case.
//
// A rune is not always a whole grapheme cluster -- an emoji built from a
// zero-width joiner sequence yields only its first component here. Proper
// handling would need golang.org/x/text/grapheme, adding a second dependency,
// but a single rune is the correct fix for the actual defect and a name
// beginning with a ZWJ sequence still produces valid, renderable UTF-8.
func initialOf(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "?"
	}
	first := []rune(name)[0]
	return cases.Upper(language.Und).String(string(first))
}
