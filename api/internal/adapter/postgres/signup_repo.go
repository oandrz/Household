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

// SignupRepo implements usecase.SignupRepository. It keeps the pool
// alongside the pool-backed *sqlcgen.Queries, like InviteRepo and UserRepo,
// because Provision needs to begin its own transaction.
type SignupRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewSignupRepo(db *DB) *SignupRepo {
	return &SignupRepo{pool: db.Pool(), q: sqlcgen.New(db.Pool())}
}

func (r *SignupRepo) Create(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateSignup(ctx, sqlcgen.CreateSignupParams{
		Email:     text(email),
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create signup")
}

// CreateConsumed writes a row via CreateConsumedSignup (see its doc comment
// in queries/signup.sql): it makes CountForEmailSince/CountSince advance for
// a registered address, the same way Create advances them for a fresh one.
func (r *SignupRepo) CreateConsumed(ctx context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateConsumedSignup(ctx, sqlcgen.CreateConsumedSignupParams{
		Email:     text(email),
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(expiresAt),
	}), "create consumed signup")
}

// CreateForTelegram is CreateSignup's Telegram twin -- see the doc comment on
// usecase.SignupRepository.CreateForTelegram for why the two are mutually
// exclusive per row.
func (r *SignupRepo) CreateForTelegram(ctx context.Context, chatID int64, tokenHash []byte, expiresAt time.Time) error {
	return translate(r.q.CreateTelegramSignup(ctx, sqlcgen.CreateTelegramSignupParams{
		TelegramChatID: &chatID,
		TokenHash:      tokenHash,
		ExpiresAt:      timestamptz(expiresAt),
	}), "create telegram signup")
}

func (r *SignupRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.SignupDetails, error) {
	row, err := r.q.GetSignupByTokenHash(ctx, tokenHash)
	if err != nil {
		return usecase.SignupDetails{}, translate(err, "get signup by token hash")
	}
	return usecase.SignupDetails{
		ID:             uuidToString(row.ID),
		Email:          stringOrEmpty(row.Email),
		TelegramChatID: row.TelegramChatID,
		ExpiresAt:      timeOf(row.ExpiresAt),
		ConsumedAt:     timePtrOf(row.ConsumedAt),
	}, nil
}

func (r *SignupRepo) CountForEmailSince(ctx context.Context, email string, since time.Time) (int, error) {
	n, err := r.q.CountSignupsForEmailSince(ctx, sqlcgen.CountSignupsForEmailSinceParams{
		Email:     text(email),
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

// Provision creates a whole household in one transaction: household, owner,
// membership, the three builtin spaces and notification preferences, with
// the signup stamped consumed first.
//
// ConsumeSignup runs before any insert so its guarded UPDATE serialises two
// concurrent completions of the same token -- the loser writes nothing and
// gets domain.ErrTokenExpired. Running it last would let both callers
// create a household.
//
// Never split this into separate repository calls -- a failure partway
// through would orphan a users row blocking users.email's unique index with
// no membership under it, permanently locking that address out. (Same risk
// InviteRepository.Accept guards against.)
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

	// The guard is in the SQL (consumed_at IS NULL AND expires_at > now()):
	// this single statement both claims the signup and tells us whether it
	// was claimable. It returns the email, so the verified address reaches
	// the user row without a caller substituting a different one.
	claimed, err := q.ConsumeSignup(ctx, uuid(signupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Consumed or expired -- indistinguishable here, deliberately.
			// SignupService.Complete's TokenLifecycle read tells the two
			// apart for a caller; this answer is authoritative only for the
			// race between that read and this write.
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
		Timezone:              b.Timezone,
	})
	if err != nil {
		return usecase.ProvisionedHousehold{}, translate(err, "create household for signup")
	}
	householdID := uuidToString(householdRow.ID)

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		// claimed.Email is already the pointer sqlc's CreateUserParams.Email
		// expects, nil for NULL -- signups.email is nullable for exactly the
		// same reason users.email is, so no "" <-> NULL conversion is needed
		// here the way nullableText(passwordHash) needs it below.
		Email:         claimed.Email,
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

	// Bind the chat from the claimed row, never from a caller -- the same
	// rule ConsumeSignup's comment states for the email.
	//
	// It happens inside this transaction because a household left with its
	// chat unbound is an account its owner can never sign into again: the
	// token is already spent.
	if claimed.TelegramChatID != nil {
		if err := q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
			UserID: userRow.ID,
			ChatID: *claimed.TelegramChatID,
			// A Telegram sign-up has no confirm screen to show a name on --
			// nothing here ever reads it, so the update's from.username is
			// deliberately left unused.
			ChatUsername: nullableText(""),
		}); err != nil {
			return usecase.ProvisionedHousehold{}, translate(err, "bind telegram account for signup")
		}
	}

	// domain.BuiltinSpaces runs here, inside the transaction, because it
	// needs the household ID that the insert above just created. Domain owns
	// which spaces a household starts with; this only executes that.
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
