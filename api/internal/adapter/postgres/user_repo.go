package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint
// violation (23505). See
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const pgUniqueViolation = "23505"

// categoryNameUniqueConstraint is the name Postgres gave categories' own
// UNIQUE (household_id, name) (migrations/00005_transactions.sql), Postgres's
// default naming for an unnamed table constraint: "<table>_<columns>_key".
// translate checks this by name, not only by SQLSTATE 23505, so a future
// unique key on categories (or any other 23505 whose message happens to
// mention the table) cannot masquerade as a name collision.
const categoryNameUniqueConstraint = "categories_household_id_name_key"

// translate converts driver errors into domain errors so nothing above the
// adapter layer ever sees pgx types.
func translate(err error, op string) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	case errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == categoryNameUniqueConstraint:
		// CategoryRepository's own contract (usecase/ports.go) wants a
		// sentinel specific to this one constraint, not the generic
		// ErrAlreadyExists below -- Create and Rename both hit this on a
		// name collision, archived rows included, since archived_at is not
		// part of the unique key.
		return fmt.Errorf("%s: constraint %q: %w", op, pgErr.ConstraintName, domain.ErrCategoryNameTaken)
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
		//
		// op and pgErr.ConstraintName are folded into the message -- not
		// just discarded the way a bare `return domain.ErrAlreadyExists`
		// would -- because this is the one class of error with a typed
		// sentinel a caller can match against, which makes it exactly the
		// case where losing the diagnostic (which operation, which
		// constraint) would be missed most: every log line for it would
		// otherwise read "already exists" with no way to tell CreateSpace's
		// key collision apart from CreateUser's email collision. %w keeps it
		// errors.Is-matchable against domain.ErrAlreadyExists despite the
		// wrapping, exactly as the default branch below already does for
		// every other error.
		return fmt.Errorf("%s: constraint %q: %w", op, pgErr.ConstraintName, domain.ErrAlreadyExists)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
