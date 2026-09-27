package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadOnlyDB is the second connection pool: the one the admin database
// browse reads through, built from DATABASE_READONLY_URL.
//
// It is its own type, not another *DB, so the compiler -- not convention --
// stops a repository being handed the wrong pool. Only the browse takes a
// *ReadOnlyDB, and it takes nothing else.
type ReadOnlyDB struct {
	pool *pgxpool.Pool
}

// ErrReadOnlyMisconfigured marks failures a human caused that no retry
// fixes: an unparseable DSN, or a connection that can write. main.go
// refuses the boot only on these -- not a database that isn't up yet, a
// host that doesn't resolve, or a missing role, since those happen during
// a fresh-box restore and refusing the boot there would take the whole
// household product down over an admin panel.
var ErrReadOnlyMisconfigured = errors.New("DATABASE_READONLY_URL is misconfigured")

// browseStatementTimeout bounds every statement this pool runs, in
// milliseconds -- the unit Postgres uses for an unsuffixed
// statement_timeout.
//
// It's set here and on the hearth_readonly role itself
// (deploy/readonly-role.sql); they fail independently, so a box provisioned
// from an older PROVISION.md without the role setting is still bounded.
const browseStatementTimeout = "3000"

// browseMaxConns is small: one operator uses this panel, unlike Open's
// MaxConns of 10 sized for product traffic. A runaway browse must not be
// able to take connections the household product needs.
const browseMaxConns = 3

// OpenReadOnly builds the browse's pool and refuses to return one that can
// write.
//
// The privilege check runs in AfterConnect, so it holds for every
// connection the pool ever opens, not just the first -- it stays true after
// a reconnect or a role change at 2 a.m., unlike a boot-time-only check.
// Ping below forces the first connection, so a wrong URL fails here rather
// than on the first operator request.
func OpenReadOnly(ctx context.Context, databaseURL string) (*ReadOnlyDB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse DATABASE_READONLY_URL: %v", ErrReadOnlyMisconfigured, err)
	}
	cfg.MaxConns = browseMaxConns
	cfg.MaxConnLifetime = time.Hour
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = browseStatementTimeout
	cfg.AfterConnect = assertCannotWrite

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create the read-only pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping the read-only database: %w", err)
	}
	return &ReadOnlyDB{pool: pool}, nil
}

// assertCannotWrite refuses any connection that could modify the product's
// data.
//
// users is the probe table: it exists since migration 00002, is never
// dropped, and holds credentials, so a connection that can write there is
// disqualified regardless of anything else. It's qualified as public.users,
// not users, so the guard can't be pointed at a same-named relation earlier
// on search_path -- browse_repo.go qualifies every relation it reads for the
// same reason. The check is a privilege lookup, not an attempted write, so
// it leaves nothing behind even on a connection it rejects.
//
// An error reading the privilege counts as a refusal too, not a pass --
// CLAUDE.md's fail-closed rule.
func assertCannotWrite(ctx context.Context, conn *pgx.Conn) error {
	var canWrite bool
	err := conn.QueryRow(ctx,
		`SELECT has_table_privilege(current_user, 'public.users', 'INSERT')`).Scan(&canWrite)
	if err != nil {
		return fmt.Errorf("could not check whether DATABASE_READONLY_URL is read-only: %w", err)
	}
	if canWrite {
		return fmt.Errorf("%w: DATABASE_READONLY_URL connects as a role that may INSERT into users, "+
			"so it is not a read-only role. Point it at hearth_readonly (deploy/readonly-role.sql)",
			ErrReadOnlyMisconfigured)
	}
	return nil
}

func (db *ReadOnlyDB) Pool() *pgxpool.Pool { return db.pool }
func (db *ReadOnlyDB) Close()              { db.pool.Close() }
