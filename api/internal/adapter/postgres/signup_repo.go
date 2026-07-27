package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// SignupRepo implements usecase.SignupRepository. It keeps the pool alongside
// the pool-backed *sqlcgen.Queries, just as InviteRepo and UserRepo do,
// because Provision needs to begin its own transaction -- something a
// *sqlcgen.Queries built once at construction time cannot do on its own.
type SignupRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewSignupRepo(db *DB) *SignupRepo {
	return &SignupRepo{pool: db.Pool(), q: sqlcgen.New(db.Pool())}
}

func (r *SignupRepo) Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateSignup(ctx, sqlcgen.CreateSignupParams{
		Email:     email,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create signup")
}

// CreateConsumed writes a row via CreateConsumedSignup, whose own doc comment
// (queries/signup.sql) explains why: it is what makes
// CountForEmailSince/CountSince advance for a registered address, the same
// way Create advances them for a fresh one.
func (r *SignupRepo) CreateConsumed(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateConsumedSignup(ctx, sqlcgen.CreateConsumedSignupParams{
		Email:     email,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create consumed signup")
}

func (r *SignupRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.SignupDetails, error) {
	row, err := r.q.GetSignupByTokenHash(ctx, tokenHash)
	if err != nil {
		return usecase.SignupDetails{}, translate(err, "get signup by token hash")
	}
	return usecase.SignupDetails{
		ID:         uuidToString(row.ID),
		Email:      row.Email,
		ExpiresAt:  timeOf(row.ExpiresAt),
		ConsumedAt: timePtrOf(row.ConsumedAt),
	}, nil
}

func (r *SignupRepo) CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error) {
	n, err := r.q.CountSignupsForEmailSince(ctx, sqlcgen.CountSignupsForEmailSinceParams{
		Email:     email,
		CreatedAt: timestamptz(since),
	})
	if err != nil {
		return 0, translate(err, "count signups for email")
	}
	return int(n), nil
}

func (r *SignupRepo) CountSince(ctx context.Context, since time.Time) (int, error) {
	n, err := r.q.CountSignupsSince(ctx, timestamptz(since))
	if err != nil {
		return 0, translate(err, "count signups")
	}
	return int(n), nil
}

// Provision creates a whole household in one transaction: the household, the
// owner, their membership, the three builtin spaces and the notification
// preferences, with the signup stamped consumed first.
//
// The ordering matters. ConsumeSignup runs before any insert, so its guarded
// UPDATE is what serialises two concurrent completions of the same token --
// the loser gets zero rows and returns domain.ErrTokenExpired having written
// nothing. Doing it last would let both callers create a household.
//
// Do not decompose this into separate repository calls. A failure between
// them would leave a users row occupying users.email's unique index with no
// membership under it, and that address could then never sign up again: a
// retry cannot create a second user with the same email, so there is no path
// forward short of manual SQL. InviteRepository.Accept exists for the
// identical reason.
func (r *SignupRepo) Provision(ctx context.Context, signupID, passwordHash string,
	b usecase.HouseholdBlueprint) (usecase.ProvisionedHousehold, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.ProvisionedHousehold{}, fmt.Errorf("begin provision transaction: %w", err)
	}
	// A no-op once Commit has succeeded; the error from a post-commit Rollback
	// is deliberately discarded, matching the standard defer-rollback pattern
	// for pgx transactions.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	// The guard is in the SQL (consumed_at IS NULL AND expires_at > now()), so
	// this single statement both claims the signup and tells us whether it was
	// claimable. It returns the email, which is how the verified address
	// reaches the user row without a caller being able to substitute a
	// different one.
	claimed, err := q.ConsumeSignup(ctx, uuid(signupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Consumed or expired -- indistinguishable here, deliberately.
			// SignupService.Complete's own TokenLifecycle read is what tells
			// the two apart for a caller; this answer is authoritative only
			// for the race between that read and this write.
			return usecase.ProvisionedHousehold{}, domain.ErrTokenExpired
		}
		return usecase.ProvisionedHousehold{}, translate(err, "consume signup")
	}

	householdRow, err := q.CreateHousehold(ctx, sqlcgen.CreateHouseholdParams{
		Name:                  b.Name,
		FamilyName:            b.FamilyName,
		PrimaryCurrency:       b.PrimaryCurrency,
		ShowSecondaryCurrency: b.ShowSecondaryCurrency,
		SecondaryCurrency:     b.SecondaryCurrency,
	})
	if err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "create household for signup")
	}
	householdID := uuidToString(householdRow.ID)

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(claimed.Email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   b.OwnerDisplayName,
		AvatarInitial: initialOf(b.OwnerDisplayName),
	})
	if err != nil {
		// A unique violation here means the address gained an account between
		// SignupService.Complete's check and this insert -- two live tokens
		// for one address, both completed. translate maps it to
		// domain.ErrAlreadyExists, which MapDomainError answers 409.
		return usecase.ProvisionedHousehold{}, translate(err, "create owner for signup")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  householdRow.ID,
		UserID:       userRow.ID,
		Role:         string(b.OwnerRole),
		Capabilities: b.OwnerCapabilities.Strings(),
	})
	if err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "create owner membership for signup")
	}

	// domain.BuiltinSpaces is called here, inside the transaction, because it
	// needs the household ID -- which does not exist until the insert above.
	// The knowledge of which spaces a household starts with stays in domain;
	// this only executes it.
	for _, s := range domain.BuiltinSpaces(householdID) {
		if _, err := q.CreateSpace(ctx, sqlcgen.CreateSpaceParams{
			HouseholdID:        householdRow.ID,
			Key:                s.Key,
			Name:               s.Name,
			Visibility:         string(s.Visibility),
			Position:           int32(s.Position),
			IsBuiltin:          s.IsBuiltin,
			RequiredCapability: string(s.RequiredCapability),
		}); err != nil {
			return usecase.ProvisionedHousehold{}, translate(err, fmt.Sprintf("create builtin space %q", s.Key))
		}
	}

	if _, err := q.UpsertNotificationPreferences(ctx, sqlcgen.UpsertNotificationPreferencesParams{
		HouseholdID:     householdRow.ID,
		BillReminders:   b.Notifications.BillReminders,
		OverspendAlerts: b.Notifications.OverspendAlerts,
		RetroReminder:   b.Notifications.RetroReminder,
		WeeklyDigest:    b.Notifications.WeeklyDigest,
	}); err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "set notification preferences for signup")
	}

	if err := tx.Commit(ctx); err != nil {
		return usecase.ProvisionedHousehold{}, fmt.Errorf("commit provision transaction: %w", err)
	}

	return usecase.ProvisionedHousehold{
		UserID:       uuidToString(userRow.ID),
		HouseholdID:  householdID,
		MembershipID: uuidToString(membershipRow.ID),
	}, nil
}

func (r *SignupRepo) Prune(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.q.PruneSignups(ctx, timestamptz(before))
	if err != nil {
		return 0, translate(err, "prune signups")
	}
	return deleted, nil
}
