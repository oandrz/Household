// Package postgres holds every implementation that talks to Postgres. Nothing
// above the adapter layer imports it.
package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

const (
	// RequestPoolMaxConns is how many connections the request pool holds.
	// ProductionHouseholdLimits.InFlight is sized against it.
	RequestPoolMaxConns = 10
	// RequestStatementTimeout bounds one query on the request pool. Every
	// query a request makes reads capped data, so one running this long is
	// a defect; it must also stay below the HTTP layer's RequestDeadline.
	RequestStatementTimeout = 5 * time.Second

	// The jobs pool serves the daily prune and adminctl: few connections,
	// kept off the request pool, and a longer limit, because a delete over
	// a table that grew during a busy month may need it.
	jobsPoolMaxConns     = 2
	JobsStatementTimeout = 60 * time.Second
)

// Open builds the request pool and verifies it is usable before returning.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	return open(ctx, databaseURL, RequestPoolMaxConns, RequestStatementTimeout)
}

// OpenJobs builds the pool for background jobs; see JobsStatementTimeout.
func OpenJobs(ctx context.Context, databaseURL string) (*DB, error) {
	return open(ctx, databaseURL, jobsPoolMaxConns, JobsStatementTimeout)
}

func open(ctx context.Context, databaseURL string, maxConns int32, statementTimeout time.Duration) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = time.Hour
	// Milliseconds: the unit Postgres uses for an unsuffixed value.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(statementTimeout.Milliseconds(), 10)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }
func (db *DB) Pool() *pgxpool.Pool            { return db.pool }
func (db *DB) Close()                         { db.pool.Close() }
