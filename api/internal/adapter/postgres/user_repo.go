package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

type UserRepo struct{ q *sqlcgen.Queries }

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{q: sqlcgen.New(db.Pool())} }

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

// translate converts driver errors into domain errors so nothing above the
// adapter layer ever sees pgx types.
func translate(err error, op string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
