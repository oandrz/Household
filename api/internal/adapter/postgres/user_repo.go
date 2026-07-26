package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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
	membership := domain.Membership{
		ID:           uuidToString(membershipRow.ID),
		HouseholdID:  uuidToString(membershipRow.HouseholdID),
		UserID:       uuidToString(membershipRow.UserID),
		Role:         toRole(membershipRow.Role),
		Capabilities: toCapabilities(membershipRow.Capabilities),
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

func initialOf(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "?"
	}
	return strings.ToUpper(name[:1])
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint
// violation (23505). See
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const pgUniqueViolation = "23505"

// translate converts driver errors into domain errors so nothing above the
// adapter layer ever sees pgx types.
func translate(err error, op string) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	case errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation:
		// Mirrors ErrNotFound's translation: a caller-testable domain
		// sentinel rather than a generic wrapped driver error, so
		// usecase-level code can distinguish "this already exists" from
		// any other failure with errors.Is. Task 15's CreateSpace is the
		// first caller: its own pre-check (list, then compare keys) closes
		// the common case, but two concurrent creates deriving the same
		// key can both pass that check before either insert lands, and the
		// database's UNIQUE (household_id, key) constraint is the
		// authoritative backstop for that race.
		return domain.ErrAlreadyExists
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
