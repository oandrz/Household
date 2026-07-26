package postgres_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMarkInviteAcceptedIsSingleUse proves the query guards against two
// concurrent accept requests both succeeding: the first call transitions the
// invite and returns its id, and a second call against the same,
// now-accepted invite matches zero rows.
func TestMarkInviteAcceptedIsSingleUse(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	queries := sqlcgen.New(db.Pool())

	_, _, inviteID := seedInvite(t, db, ctx, time.Now().Add(time.Hour))

	got, err := queries.MarkInviteAccepted(ctx, inviteID)
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if got != inviteID {
		t.Fatalf("returned id %v, want %v", got, inviteID)
	}

	_, err = queries.MarkInviteAccepted(ctx, inviteID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("re-accepting an already-accepted invite: got %v, want pgx.ErrNoRows", err)
	}
}

// TestMarkInviteAcceptedRejectsAnExpiredInvite proves an invite past its
// expires_at can never be accepted, even on the very first call.
func TestMarkInviteAcceptedRejectsAnExpiredInvite(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	queries := sqlcgen.New(db.Pool())

	_, _, inviteID := seedInvite(t, db, ctx, time.Now().Add(-time.Hour))

	_, err := queries.MarkInviteAccepted(ctx, inviteID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("accepting an expired invite: got %v, want pgx.ErrNoRows", err)
	}
}

// seedInvite inserts a household, a user, and an invite from that user
// expiring at expiresAt, returning their ids as pgtype.UUID ready to pass to
// generated queries.
func seedInvite(t *testing.T, db *postgres.DB, ctx context.Context, expiresAt time.Time) (householdID, userID, inviteID pgtype.UUID) {
	t.Helper()

	err := db.Pool().QueryRow(ctx,
		`INSERT INTO households (name, family_name) VALUES ('Andreas & Christine', 'Oentoro') RETURNING id`).
		Scan(&householdID)
	if err != nil {
		t.Fatalf("insert household: %v", err)
	}

	err = db.Pool().QueryRow(ctx,
		`INSERT INTO users (display_name, avatar_initial) VALUES ('Andreas', 'A') RETURNING id`).
		Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	tokenHash := make([]byte, 32)
	if _, err := rand.Read(tokenHash); err != nil {
		t.Fatalf("generate token hash: %v", err)
	}

	err = db.Pool().QueryRow(ctx,
		`INSERT INTO invites (household_id, email, name, role, capabilities, token_hash, invited_by, expires_at)
		 VALUES ($1, 'kid@example.com', 'Kid', 'limited', ARRAY['calendar'], $2, $3, $4)
		 RETURNING id`,
		householdID, tokenHash, userID, expiresAt).
		Scan(&inviteID)
	if err != nil {
		t.Fatalf("insert invite: %v", err)
	}

	return householdID, userID, inviteID
}
