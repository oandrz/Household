package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadOnlyDB is the second connection pool: the one the admin database browse
// reads through, built from DATABASE_READONLY_URL.
//
// It is its own type rather than another *DB on purpose. Both wrap a
// *pgxpool.Pool and nothing in their shapes would stop a repository being
// handed the wrong one -- so the compiler is made to care instead. Nothing
// but the browse takes a *ReadOnlyDB, and the browse takes nothing else.
type ReadOnlyDB struct {
	pool *pgxpool.Pool
}

// ErrReadOnlyMisconfigured marks the failures a human caused and no retry
// fixes: a DSN that cannot be parsed, and a connection that turns out to be
// able to write. main.go refuses the boot on these and only these.
//
// Everything else -- a database that is not up yet, a host that does not
// resolve, a role that does not exist -- is deliberately NOT this. The day
// that happens is the day someone is restoring onto a fresh box with the
// variable already in .env, and refusing the boot there would take the whole
// household product down over an operator panel.
var ErrReadOnlyMisconfigured = errors.New("DATABASE_READONLY_URL is misconfigured")

// browseStatementTimeout bounds every statement this pool runs, in
// milliseconds because that is the unit Postgres reads an unsuffixed
// statement_timeout in.
//
// It is set here as well as on the hearth_readonly role itself (see
// deploy/readonly-role.sql). Two mechanisms for one rule is right here
// because they fail independently: a box provisioned from an older
// PROVISION.md has a role without the setting, and this still bounds it.
const browseStatementTimeout = "3000"

// browseMaxConns is small because one operator clicks this panel. Open's
// MaxConns of 10 is sized for the product's request traffic; giving the same
// budget to an admin screen would let a runaway browse take connections the
// household product needs.
const browseMaxConns = 3

// OpenReadOnly builds the browse's pool and refuses to return one that can
// write.
//
// The privilege check runs in AfterConnect, so it holds for every connection
// the pool ever opens rather than only for the first: a boot-time-only check
// is a statement about the process that started, and this one stays true
// after a reconnect and after somebody grants the role something at 2 a.m.
// Ping below is what forces the first connection, so a wrong URL fails here
// and not on the first operator request.
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
// users is the probe table because it exists in migration 00002, is never
// dropped, and holds credentials: if this connection can write *there*,
// nothing else about the configuration matters. The check is a privilege
// lookup and not an attempted write, so it leaves nothing behind even on the
// connection it rejects.
//
// All three outcomes are distinct and only one of them is a pass. An error
// reading the privilege is a refusal too -- "I could not tell" is not
// "read-only, fine", and this is exactly the value CLAUDE.md's fail-closed
// rule is about.
func assertCannotWrite(ctx context.Context, conn *pgx.Conn) error {
	var canWrite bool
	err := conn.QueryRow(ctx,
		`SELECT has_table_privilege(current_user, 'users', 'INSERT')`).Scan(&canWrite)
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
