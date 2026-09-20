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
		// address -- CreateTelegram below is the repository's other path,
		// for the invite with none -- and nullableText would turn "" into
		// NULL, which invites_channel_matches_email then refuses against
		// the default channel of 'email'.
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

// CreateTelegram writes an invite with no email column touched at all --
// not even as an explicit NULL -- because CreateTelegramInvite's own INSERT
// never names the column, letting it take its schema default. That is what
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

// MarkAccepted deliberately does not go through translate. MarkInviteAccepted
// is a guarded atomic update (accepted_at IS NULL AND expires_at > now()), so
// zero rows means the invite was already accepted or has expired -- not that
// no invite with this id ever existed. translate's generic
// pgx.ErrNoRows -> domain.ErrNotFound mapping would misreport that as "no
// such invite"; the task's constraints call for domain.ErrInviteAlreadyAccepted
// instead, so that mapping is applied directly here.
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

// Accept runs the guarded MarkInviteAccepted update, the user insert, and the
// membership insert in a single transaction, so a household of two is never
// left with an invite that can neither be accepted nor retried.
//
// MarkInviteAccepted runs first, before either insert: it is what makes a
// concurrent second acceptance of the same invite fail cheaply, as
// domain.ErrInviteAlreadyAccepted, before any row is written -- rather than
// failing with a raw unique-constraint error from CreateUser colliding on the
// invite's email address, which the first, successful acceptance already
// claimed.
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

// Delete is one guarded statement on the common path. Only when it removed
// nothing does a second, equally household-scoped read decide which of
// InviteRepository.Delete's two refusals applies -- so neither statement can
// confirm that another household's invite id exists.
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
// does not match -- unknown token, email channel, accepted, expired, or
// already knocked. That is deliberately one answer: the caller turns it
// into the bot's one bland reply, and any difference between these cases
// would be something a chat could probe for.
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

// ReplaceToken is the single write behind "get a new link", which is also
// "Not them": one statement replaces the token and clears the knock
// together, so there is never an instant where a fresh link carries a
// stale knock. It returns the chat that had knocked (0 when nobody had),
// read back inside the same UPDATE -- see ReplaceInviteToken's own SQL
// comment for why that is safe to trust, and
// TestReplaceInviteTokenReturnsThePreviousKnockChatID for the proof against
// a real database.
//
// Household-scoped and channel-scoped in the guarded UPDATE itself, so an
// id from another household matches nothing there and reports
// domain.ErrNotFound, the same answer an id that never existed gets -- the
// fallback read below never runs for that case, so it never has the chance
// to confirm the id exists elsewhere.
//
// An email invite also matches nothing in the guarded UPDATE, and the
// caller cannot tell that apart from "no such invite" without reading
// further -- so on a miss this reads the row back, scoped by household and
// accepted_at IS NULL exactly as the guarded UPDATE was, to report
// domain.ErrInviteNotTelegram for the case an owner can actually act on.
// That same accepted_at IS NULL scoping is what keeps an already-accepted
// Telegram invite from reaching that channel check at all: it reads as
// domain.ErrNotFound too, because "get a new link" is not something an
// accepted invite offers, the same way Withdraw's own fallback read
// distinguishes its two failure modes.
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
	// The fallback read found a live (accepted_at IS NULL) Telegram invite
	// with this id in this household, yet the guarded UPDATE above still
	// matched nothing -- a state that should be unreachable given the two
	// queries share the same WHERE conditions. Fail closed rather than
	// report success for a write that never happened (CLAUDE.md: fail
	// closed on values you did not construct).
	return 0, domain.ErrNotFound
}

// Admit is Let in (spec decision 5): the user, the membership, the
// telegram_accounts row and the acceptance stamp, in one transaction.
// Either all four happen or none do.
//
// Do not compose this from separate calls. A failure between them would
// leave a user with no membership and no email -- no unique constraint to
// make a retry fail loudly, so each retry would silently orphan another
// one. This is the same rule Accept's own doc comment gives.
//
// ClaimKnockedInvite runs first, for the reason Accept's guard does: it is
// what makes a second, concurrent Let in fail cheaply, before any row is
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

	// email and passwordHash are both nil, not "": this member was let in
	// from a Telegram knock, never typed a password, and Accept's own "" <->
	// SQL NULL convention (StoredUser's doc comment) applies the same way
	// here as it does to every other credential-less member.
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
	// (migration 00021) ties it to knocked_at: ClaimKnockedInvite's own guard
	// requires knocked_at IS NOT NULL, so it is never nil here in practice.
	// int64Or is used anyway rather than a bare dereference, because a
	// pointer this code did not itself just check must never be trusted
	// blindly (CLAUDE.md: fail closed on values you did not construct).
	chatID := int64Or(claimed.KnockChatID)
	if err := q.CreateTelegramAccount(ctx, sqlcgen.CreateTelegramAccountParams{
		UserID:       userRow.ID,
		ChatID:       chatID,
		ChatUsername: claimed.KnockChatUsername,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// The chat bound itself to a different account somewhere else
			// between the knock and this click -- the re-check spec
			// decision 15 asks for, closed by the same UNIQUE the knock-time
			// check cannot see across. translate would flatten this to the
			// generic domain.ErrAlreadyExists, because neither of
			// telegram_accounts' two UNIQUEs is a named entry in its
			// uniqueConstraintErrors map (that map exists for exactly this
			// case: an unlisted name falls through to the generic sentinel).
			// The specific domain.ErrChatAlreadyBound the caller needs is
			// mapped explicitly here instead of adding a table entry,
			// because TelegramAccountRepository.Create's own contract
			// (ports.go) deliberately keeps the generic sentinel for its
			// two UNIQUEs -- "the caller knows which side it was asking
			// about... and chooses the sentence, rather than a repository
			// guessing at intent" -- and this is that caller choosing.
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
// matched nothing, to tell its five collapsed conditions apart -- the same
// fallback-read shape Delete already uses via InviteAcceptedInHousehold.
// Household-scoped, so an id from another household reports
// domain.ErrNotFound rather than confirming it exists elsewhere. It reads
// through q, the same transaction-scoped queries ClaimKnockedInvite just
// ran through, so it sees the identical snapshot rather than opening a
// second connection mid-transaction.
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
	// invite has expired. Admit has nothing more specific to say for any of
	// those than "nobody is waiting" -- the same one-answer-for-several-
	// causes shape RecordKnock's own doc comment explains.
	return domain.ErrInviteNotKnocked
}
