package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// InviteRepo keeps the pool alongside the pool-backed *sqlcgen.Queries every
// other repository is content with, because Accept needs to begin its own
// transaction -- something a *sqlcgen.Queries built once at construction time
// cannot do on its own.
type InviteRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewInviteRepo(db *DB) *InviteRepo {
	return &InviteRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

func (r *InviteRepo) Create(ctx context.Context, householdID, email, name string, role domain.Role,
	caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error) {
	id, err := r.q.CreateInvite(ctx, sqlcgen.CreateInviteParams{
		HouseholdID: uuid(householdID),
		// text(), not nullableText(): Create's caller always has a real
		// address (CreateTelegram below handles the no-address case).
		// nullableText would turn "" into NULL, which
		// invites_channel_matches_email refuses for the default 'email'
		// channel.
		Email:        text(email),
		Name:         name,
		Role:         string(role),
		Capabilities: caps.Strings(),
		TokenHash:    tokenHash,
		InvitedBy:    uuid(invitedBy),
		ExpiresAt:    timestamptz(expiresAt),
	})
	if err != nil {
		return "", translate(err, "create invite")
	}
	return uuidToString(id), nil
}

// CreateTelegram writes an invite with no email column touched, not even
// as an explicit NULL: CreateTelegramInvite's own INSERT never names the
// column, so it takes its schema default -- what
// invites_channel_matches_email requires of a 'telegram' row.
func (r *InviteRepo) CreateTelegram(ctx context.Context, householdID, name string, role domain.Role,
	caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error) {
	id, err := r.q.CreateTelegramInvite(ctx, sqlcgen.CreateTelegramInviteParams{
		HouseholdID:  uuid(householdID),
		Name:         name,
		Role:         string(role),
		Capabilities: caps.Strings(),
		TokenHash:    tokenHash,
		InvitedBy:    uuid(invitedBy),
		ExpiresAt:    timestamptz(expiresAt),
	})
	if err != nil {
		return "", translate(err, "create telegram invite")
	}
	return uuidToString(id), nil
}

func (r *InviteRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (usecase.InviteDetails, error) {
	row, err := r.q.GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return usecase.InviteDetails{}, translate(err, "get invite by token hash")
	}
	role, err := toRole(row.Role)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	caps, err := toCapabilities(row.Capabilities)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	channel, err := domain.ParseInviteChannel(row.Channel)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	return usecase.InviteDetails{
		ID:           uuidToString(row.ID),
		HouseholdID:  uuidToString(row.HouseholdID),
		Email:        stringOrEmpty(row.Email),
		Name:         row.Name,
		Role:         role,
		Capabilities: caps,
		Channel:      channel,
		FamilyName:   row.FamilyName,
		InviterName:  row.InviterName,
		ExpiresAt:    timeOf(row.ExpiresAt),
		AcceptedAt:   timePtrOf(row.AcceptedAt),
	}, nil
}

func (r *InviteRepo) LiveInviteForEmail(ctx context.Context, householdID, email string) (usecase.InviteDetails, error) {
	row, err := r.q.GetLiveInviteForEmail(ctx, sqlcgen.GetLiveInviteForEmailParams{
		HouseholdID: uuid(householdID),
		// text(), not nullableText(): a Telegram invite has no address, so
		// this lookup is never searching for one -- the same "always a
		// real value" case text() documents for ByEmail and CountSince.
		Email: text(email),
	})
	if err != nil {
		return usecase.InviteDetails{}, translate(err, "get live invite for email")
	}
	role, err := toRole(row.Role)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	caps, err := toCapabilities(row.Capabilities)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	channel, err := domain.ParseInviteChannel(row.Channel)
	if err != nil {
		return usecase.InviteDetails{}, err
	}
	return usecase.InviteDetails{
		ID:           uuidToString(row.ID),
		HouseholdID:  uuidToString(row.HouseholdID),
		Email:        stringOrEmpty(row.Email),
		Name:         row.Name,
		Role:         role,
		Capabilities: caps,
		Channel:      channel,
		FamilyName:   row.FamilyName,
		InviterName:  row.InviterName,
		ExpiresAt:    timeOf(row.ExpiresAt),
		AcceptedAt:   timePtrOf(row.AcceptedAt),
	}, nil
}

// MarkAccepted does not go through translate. MarkInviteAccepted is a
// guarded update (accepted_at IS NULL AND expires_at > now()), so zero rows
// means already accepted or expired, not "no such invite" -- so this maps
// pgx.ErrNoRows to domain.ErrInviteAlreadyAccepted directly instead.
func (r *InviteRepo) MarkAccepted(ctx context.Context, inviteID string) error {
	_, err := r.q.MarkInviteAccepted(ctx, uuid(inviteID))
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInviteAlreadyAccepted
	}
	return fmt.Errorf("mark invite accepted: %w", err)
}

// Accept runs the guarded MarkInviteAccepted update, the user insert and the
// membership insert in one transaction, so an invite is never left
// half-accepted -- neither acceptable again nor retryable.
//
// MarkInviteAccepted runs first: it makes a concurrent second acceptance
// fail cheaply as domain.ErrInviteAlreadyAccepted before any row is written,
// instead of on CreateUser's unique-email collision with the first acceptance.
func (r *InviteRepo) Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
	householdID string, role domain.Role, caps domain.Capabilities) (usecase.AcceptedInvite, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.AcceptedInvite{}, fmt.Errorf("begin accept invite transaction: %w", err)
	}
	// A no-op once Commit has succeeded; the error from a post-commit
	// Rollback call is deliberately discarded, matching the standard
	// defer-rollback pattern for pgx transactions.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	if _, err := q.MarkInviteAccepted(ctx, uuid(inviteID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return usecase.AcceptedInvite{}, domain.ErrInviteAlreadyAccepted
		}
		return usecase.AcceptedInvite{}, fmt.Errorf("mark invite accepted: %w", err)
	}

	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nullableText(email),
		PasswordHash:  nullableText(passwordHash),
		DisplayName:   displayName,
		AvatarInitial: initialOf(displayName),
	})
	if err != nil {
		return usecase.AcceptedInvite{}, translate(err, "create user for invite acceptance")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  uuid(householdID),
		UserID:       userRow.ID,
		Role:         string(role),
		Capabilities: caps.Strings(),
	})
	if err != nil {
		return usecase.AcceptedInvite{}, translate(err, "create membership for invite acceptance")
	}

	if err := tx.Commit(ctx); err != nil {
		return usecase.AcceptedInvite{}, fmt.Errorf("commit accept invite transaction: %w", err)
	}

	return usecase.AcceptedInvite{
		UserID:       uuidToString(userRow.ID),
		MembershipID: uuidToString(membershipRow.ID),
		HouseholdID:  uuidToString(membershipRow.HouseholdID),
	}, nil
}

// ListPending reads role and capabilities through toRole and toCapabilities
// like every other enum read here, so a value this build does not know fails
// the read instead of reaching the wire (CLAUDE.md: fail closed).
func (r *InviteRepo) ListPending(ctx context.Context, householdID string, now time.Time) ([]usecase.InviteSummary, error) {
	rows, err := r.q.ListPendingInvites(ctx, sqlcgen.ListPendingInvitesParams{
		HouseholdID: uuid(householdID),
		ExpiresAt:   timestamptz(now),
	})
	if err != nil {
		return nil, translate(err, "list pending invites")
	}
	out := make([]usecase.InviteSummary, 0, len(rows))
	for _, row := range rows {
		role, err := toRole(row.Role)
		if err != nil {
			return nil, err
		}
		caps, err := toCapabilities(row.Capabilities)
		if err != nil {
			return nil, err
		}
		channel, err := domain.ParseInviteChannel(row.Channel)
		if err != nil {
			return nil, err
		}
		summary := usecase.InviteSummary{
			ID:           uuidToString(row.ID),
			Name:         row.Name,
			Email:        stringOrEmpty(row.Email), // "" for NULL -- see nullableText's inverse
			Role:         role,
			Capabilities: caps,
			Channel:      channel,
			ExpiresAt:    timeOf(row.ExpiresAt),
			CreatedAt:    timeOf(row.CreatedAt),
		}
		// knocked_at is the column invites_knock_is_whole ties the other two
		// to, so it alone decides whether there is a knock to report.
		if row.KnockedAt.Valid {
			summary.Knock = &usecase.InviteKnock{
				Username:  stringOrEmpty(row.KnockChatUsername),
				Code:      stringOrEmpty(row.KnockCode),
				KnockedAt: timeOf(row.KnockedAt),
			}
		}
		out = append(out, summary)
	}
	return out, nil
}

// Delete is one guarded statement on the common path. Only when it removes
// nothing does a second, household-scoped read decide which of its two
// refusals applies -- so neither statement can confirm another household's
// invite id exists.
func (r *InviteRepo) Delete(ctx context.Context, householdID, inviteID string) error {
	_, err := r.q.DeleteUnacceptedInvite(ctx, sqlcgen.DeleteUnacceptedInviteParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("delete invite: %w", err)
	}
	accepted, err := r.q.InviteAcceptedInHousehold(ctx, sqlcgen.InviteAcceptedInHouseholdParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return translate(err, "read invite acceptance")
	}
	if accepted {
		return domain.ErrInviteAlreadyAccepted
	}
	// An unaccepted row here means it changed between the two statements.
	// Answer as the DELETE did: nothing was removed.
	return domain.ErrNotFound
}

// RecordKnock reports domain.ErrNotFound for every case its guarded UPDATE
// misses -- unknown token, email channel, accepted, expired, or already
// knocked. That is deliberate: distinguishing them would let a chat probe
// for which case applies.
func (r *InviteRepo) RecordKnock(ctx context.Context, tokenHash []byte, chatID int64,
	username, code string, now time.Time) error {
	_, err := r.q.RecordInviteKnock(ctx, sqlcgen.RecordInviteKnockParams{
		TokenHash:         tokenHash,
		KnockChatID:       nullableInt8(chatID),
		KnockChatUsername: nullableText(username),
		KnockCode:         nullableText(code),
		KnockedAt:         timestamptz(now),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("record invite knock: %w", err)
	}
	return nil
}

// ReplaceToken is "get a new link" / "Not them": one statement replaces the
// token and clears the knock together, so a fresh link never carries a
// stale knock. It returns the previous knock chat id (0 if none), read
// back in the same UPDATE -- see ReplaceInviteToken's own SQL comment and
// TestReplaceInviteTokenReturnsThePreviousKnockChatID.
//
// The guarded UPDATE is household- and channel-scoped, so a wrong-household
// id matches nothing and reports domain.ErrNotFound, the same as an id
// that never existed -- the fallback read below never runs for that case,
// so it can't confirm the id exists elsewhere.
//
// On any other miss (an email invite, or an already-accepted Telegram
// invite), the fallback read re-checks with the same household and
// accepted_at IS NULL scoping. An email invite reports
// domain.ErrInviteNotTelegram; an already-accepted invite reports
// domain.ErrNotFound too, since an accepted invite has no link left to
// replace (the same second-read shape Delete uses to tell its own two
// refusals apart).
func (r *InviteRepo) ReplaceToken(ctx context.Context, householdID, inviteID string,
	tokenHash []byte, expiresAt time.Time) (int64, error) {
	previous, err := r.q.ReplaceInviteToken(ctx, sqlcgen.ReplaceInviteTokenParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
		TokenHash:   tokenHash,
		ExpiresAt:   timestamptz(expiresAt),
	})
	if err == nil {
		return previous, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("replace invite token: %w", err)
	}

	channel, err := r.q.InviteChannelForReplace(ctx, sqlcgen.InviteChannelForReplaceParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.ErrNotFound
		}
		return 0, translate(err, "read invite channel for replace")
	}
	if channel != string(domain.ChannelTelegram) {
		return 0, domain.ErrInviteNotTelegram
	}
	// The fallback read found a live Telegram invite matching this id and
	// household, yet the guarded UPDATE above -- which shares the same WHERE
	// conditions -- still matched nothing. That should be unreachable, so
	// fail closed rather than report success for a write that never
	// happened (CLAUDE.md: fail closed on values you did not construct).
	return 0, domain.ErrNotFound
}

// Admit is "Let in": the user, the membership, the telegram_accounts row
// and the acceptance stamp, all in one transaction. Either all four happen
// or none do.
//
// Do not compose this from separate calls: a failure between them would
// leave a user with no membership and no email, with no unique constraint
// to make a retry fail loudly -- each retry would silently orphan another
// user. This is the same rule Accept's own doc comment gives.
//
// ClaimKnockedInvite runs first, for the reason Accept's guard does: it
// makes a concurrent second Let in fail cheaply, before any row is
// written, rather than failing on the telegram_accounts unique index with
// an error nobody can map.
func (r *InviteRepo) Admit(ctx context.Context, householdID, inviteID string, now time.Time) (usecase.AdmittedInvite, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return usecase.AdmittedInvite{}, fmt.Errorf("begin admit invite transaction: %w", err)
	}
	// A no-op once Commit has succeeded; the error from a post-commit
	// Rollback call is deliberately discarded, matching Accept's own
	// defer-rollback pattern.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)

	claimed, err := q.ClaimKnockedInvite(ctx, sqlcgen.ClaimKnockedInviteParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
		AcceptedAt:  timestamptz(now),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return usecase.AdmittedInvite{}, fmt.Errorf("claim knocked invite: %w", err)
		}
		return usecase.AdmittedInvite{}, r.admitFailureReason(ctx, q, householdID, inviteID)
	}

	role, err := toRole(claimed.Role)
	if err != nil {
		return usecase.AdmittedInvite{}, err
	}
	caps, err := toCapabilities(claimed.Capabilities)
	if err != nil {
		return usecase.AdmittedInvite{}, err
	}

	// email and passwordHash are nil, not "": this member never had an
	// email or password, and the "" <-> SQL NULL convention (StoredUser's
	// doc comment) applies here the same as for every credential-less member.
	userRow, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:         nil,
		PasswordHash:  nil,
		DisplayName:   claimed.Name,
		AvatarInitial: initialOf(claimed.Name),
	})
	if err != nil {
		return usecase.AdmittedInvite{}, translate(err, "create user for invite admission")
	}

	membershipRow, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		HouseholdID:  uuid(householdID),
		UserID:       userRow.ID,
		Role:         string(role),
		Capabilities: caps.Strings(),
	})
	if err != nil {
		return usecase.AdmittedInvite{}, translate(err, "create membership for invite admission")
	}

	// knock_chat_id is nullable at the schema level, but invites_knock_is_whole
	// (migration 00021) ties it to knocked_at, and ClaimKnockedInvite's guard
	// requires knocked_at IS NOT NULL -- so it is never nil here in practice.
	// int64Or is used anyway: a pointer this code did not itself just check
	// must never be trusted blindly (CLAUDE.md: fail closed).
	chatID := int64Or(claimed.KnockChatID)
	if err := q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
		UserID:       userRow.ID,
		ChatID:       chatID,
		ChatUsername: claimed.KnockChatUsername,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// The chat bound itself to a different account between the
			// knock and this click. A chat linked to any Hearth account
			// cannot be let in, so Let in re-checks inside its transaction;
			// the same UNIQUE the knock-time check cannot see across is
			// what catches it here. translate would flatten this to the
			// generic domain.ErrAlreadyExists, since neither of telegram_accounts'
			// two UNIQUEs is in uniqueConstraintErrors (an unlisted name
			// falls through to that generic sentinel on purpose).
			// domain.ErrChatAlreadyBound is mapped explicitly here instead
			// of adding a table entry, because
			// TelegramAccountRepository.Create's own contract keeps the
			// generic sentinel for its two UNIQUEs -- the caller knows
			// which side it was asking about, so it chooses the sentence.
			return usecase.AdmittedInvite{}, domain.ErrChatAlreadyBound
		}
		return usecase.AdmittedInvite{}, fmt.Errorf("create telegram account for invite admission: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return usecase.AdmittedInvite{}, fmt.Errorf("commit admit invite transaction: %w", err)
	}

	return usecase.AdmittedInvite{
		UserID:       uuidToString(userRow.ID),
		MembershipID: uuidToString(membershipRow.ID),
		Name:         claimed.Name,
		Role:         role,
		Capabilities: caps,
		ChatID:       chatID,
	}, nil
}

// admitFailureReason runs only after ClaimKnockedInvite's guarded UPDATE
// matches nothing, to tell its five collapsed conditions apart -- the same
// fallback-read shape Delete uses via InviteAcceptedInHousehold.
// Household-scoped, so a cross-household id reports domain.ErrNotFound
// rather than confirming it exists elsewhere. It reads through q, the same
// transaction-scoped queries, so it sees the identical snapshot instead of
// opening a second connection mid-transaction.
func (r *InviteRepo) admitFailureReason(ctx context.Context, q *sqlcgen.Queries, householdID, inviteID string) error {
	accepted, err := q.InviteAcceptedInHousehold(ctx, sqlcgen.InviteAcceptedInHouseholdParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return translate(err, "read invite admit state")
	}
	if accepted {
		return domain.ErrInviteAlreadyAccepted
	}
	// Not accepted, yet ClaimKnockedInvite still matched nothing: either
	// nobody has knocked, the knock was cleared by a fresh link, or the
	// invite has expired. Admit has nothing more specific to say than
	// "nobody is waiting" -- the same one-answer-for-several-causes shape
	// RecordKnock's own doc comment explains.
	return domain.ErrInviteNotKnocked
}
