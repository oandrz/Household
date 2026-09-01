package postgres_test

import (
	"context"
	"testing"
)

// TestPlatformAdminRowsFollowTheirUser proves the ON DELETE CASCADE: an admin
// grant must not outlive the user it names. A stale row here would be an
// orphan whose user_id could, in principle, be reissued.
func TestPlatformAdminRowsFollowTheirUser(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t).Pool()

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name, avatar_initial) VALUES ('admin@example.test', 'Admin', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO platform_admins (user_id, note) VALUES ($1, 'the operator')`, userID,
	); err != nil {
		t.Fatalf("insert platform admin: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM platform_admins WHERE user_id = $1`, userID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("platform_admins rows after deleting the user = %d, want 0", remaining)
	}
}

// TestAuditRowsNeedOnlyAnActorAndAnAction proves the defaults on target,
// detail and ip: a row can be inserted with only an actor and an action, so
// a future writer that has nothing meaningful for one of these columns is
// never forced to invent a value just to satisfy a NOT NULL with no default.
func TestAuditRowsNeedOnlyAnActorAndAnAction(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t).Pool()

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name, avatar_initial) VALUES ('auditor@example.test', 'Auditor', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO admin_audit_log (actor_user_id, action) VALUES ($1, 'GET /admin/flags')`,
		userID,
	); err != nil {
		t.Fatalf("insert audit row: %v", err)
	}

	var target, ip string
	var detail []byte
	if err := pool.QueryRow(ctx,
		`SELECT target, detail, ip FROM admin_audit_log WHERE actor_user_id = $1`, userID,
	).Scan(&target, &detail, &ip); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if target != "" || ip != "" || string(detail) != "{}" {
		t.Fatalf("defaults = target %q, detail %q, ip %q; want empty, {}, empty", target, detail, ip)
	}
}
