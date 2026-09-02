package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// AdminDirectoryRepo implements usecase.AdminDirectoryRepository over
// queries/admin_directory.sql. It holds the pool as well as the queries
// because Metrics runs its four counts inside one read-only transaction.
type AdminDirectoryRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewAdminDirectoryRepo(db *DB) *AdminDirectoryRepo {
	return &AdminDirectoryRepo{pool: db.Pool(), q: sqlcgen.New(db.Pool())}
}

// Compile-time confirmation that AdminDirectoryRepo satisfies its port, the
// same check convert.go keeps for every other repository -- kept here
// rather than added to convert.go because this task's scope is this file
// and its SQL and test alone.
var _ usecase.AdminDirectoryRepository = (*AdminDirectoryRepo)(nil)

// Metrics runs the four counts at REPEATABLE READ so the four tiles
// describe one instant. Invisible on this install's size, and free.
func (r *AdminDirectoryRepo) Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (usecase.DirectoryMetrics, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return usecase.DirectoryMetrics{}, fmt.Errorf("begin metrics transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit

	q := r.q.WithTx(tx)
	households, err := q.CountHouseholds(ctx)
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count households")
	}
	active, err := q.CountActiveHouseholdsSince(ctx, timestamptz(activeSince))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count active households")
	}
	// Named CountSignupsSinceForAdmin, not CountSignupsSince: queries/signup.sql
	// already has a single-column CountSignupsSince used for per-address rate
	// limiting, and sqlc rejects a duplicate query name across files even
	// though the two return different shapes.
	signups, err := q.CountSignupsSinceForAdmin(ctx, timestamptz(signupsSince))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count signups")
	}
	pending, err := q.CountPendingInvites(ctx, timestamptz(now))
	if err != nil {
		return usecase.DirectoryMetrics{}, translate(err, "count pending invites")
	}
	if err := tx.Commit(ctx); err != nil {
		return usecase.DirectoryMetrics{}, fmt.Errorf("commit metrics transaction: %w", err)
	}
	return usecase.DirectoryMetrics{
		Households:       int(households),
		ActiveHouseholds: int(active),
		SignupsRequested: int(signups.Requested),
		SignupsCompleted: int(signups.Completed),
		PendingInvites:   int(pending),
	}, nil
}

// likePattern wraps q for ILIKE and escapes its wildcards, so a search for
// "_" finds households with an underscore rather than every household.
// Postgres's default ESCAPE character is the backslash.
func likePattern(q string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + escaped + "%"
}

func (r *AdminDirectoryRepo) SearchHouseholds(ctx context.Context, q string, limit int, _ time.Time) ([]usecase.HouseholdListing, error) {
	rows, err := r.q.SearchHouseholds(ctx, sqlcgen.SearchHouseholdsParams{
		HasQuery: q != "",
		Pattern:  likePattern(q),
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, translate(err, "search households")
	}
	out := make([]usecase.HouseholdListing, 0, len(rows))
	for _, row := range rows {
		listing := usecase.HouseholdListing{
			ID:              uuidToString(row.ID),
			Name:            row.Name,
			FamilyName:      row.FamilyName,
			MemberCount:     int(row.MemberCount),
			CreatedAt:       timeOf(row.CreatedAt),
			LastActiveAt:    timePtrOf(row.LastActiveAt),
			PrimaryCurrency: row.PrimaryCurrency,
		}
		// The lateral join names a matching member for every row of a
		// non-empty search; it is only a *reason the row appeared* when the
		// household's own fields did not match.
		if !row.HouseholdMatched && row.MatchName != nil {
			listing.Match = &usecase.MemberMatch{Name: *row.MatchName, Email: row.MatchEmail}
		}
		out = append(out, listing)
	}
	return out, nil
}

func (r *AdminDirectoryRepo) Household(ctx context.Context, householdID string, now time.Time) (usecase.HouseholdDetail, error) {
	h, err := r.q.GetHouseholdForAdmin(ctx, uuid(householdID))
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "get household for admin")
	}
	memberRows, err := r.q.ListMembersForAdmin(ctx, uuid(householdID))
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "list members for admin")
	}
	inviteRows, err := r.q.ListPendingInvitesForAdmin(ctx, sqlcgen.ListPendingInvitesForAdminParams{
		HouseholdID: uuid(householdID),
		ExpiresAt:   timestamptz(now),
	})
	if err != nil {
		return usecase.HouseholdDetail{}, translate(err, "list pending invites for admin")
	}

	detail := usecase.HouseholdDetail{
		ID:              uuidToString(h.ID),
		Name:            h.Name,
		FamilyName:      h.FamilyName,
		CreatedAt:       timeOf(h.CreatedAt),
		PrimaryCurrency: h.PrimaryCurrency,
		Members:         make([]usecase.HouseholdMember, 0, len(memberRows)),
		PendingInvites:  make([]usecase.PendingInvite, 0, len(inviteRows)),
	}
	for _, row := range memberRows {
		// Role and capabilities arrive from columns; parse them rather than
		// cast, so a value nothing in the domain constructed is refused
		// here rather than rendered.
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("member %s: %w", uuidToString(row.UserID), err)
		}
		caps, err := domain.ParseCapabilities(row.Capabilities)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("member %s: %w", uuidToString(row.UserID), err)
		}
		channel := usecase.ChannelEmail
		if row.HasTelegram {
			channel = usecase.ChannelTelegram
		}
		detail.Members = append(detail.Members, usecase.HouseholdMember{
			UserID:       uuidToString(row.UserID),
			Name:         row.DisplayName,
			Email:        row.Email,
			Channel:      channel,
			Role:         role,
			Capabilities: caps,
			LastActiveAt: timePtrOf(row.LastActiveAt),
		})
	}
	for _, row := range inviteRows {
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return usecase.HouseholdDetail{}, fmt.Errorf("invite for %s: %w", row.Email, err)
		}
		detail.PendingInvites = append(detail.PendingInvites, usecase.PendingInvite{
			Name:          row.Name,
			Email:         row.Email,
			Role:          role,
			InvitedByName: row.InvitedByName,
			ExpiresAt:     timeOf(row.ExpiresAt),
		})
	}
	return detail, nil
}
