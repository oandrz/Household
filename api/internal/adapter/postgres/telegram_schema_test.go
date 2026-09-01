package postgres_test

import (
	"context"
	"strings"
	"testing"
)

// TestSignupsRefuseBothChannels and its sibling pin the fail-closed half of
// the schema: the constraint, not the Go code, is what refuses a row that
// names two channels or none.
func TestSignupsRefuseBothChannels(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t).Pool()

	_, err := pool.Exec(ctx,
		`INSERT INTO signups (email, telegram_chat_id, token_hash, expires_at)
		 VALUES ('a@b.co', 12345, '\x00', now() + interval '1 hour')`)
	if err == nil {
		t.Fatal("insert with both channels succeeded, want constraint violation")
	}
	if !strings.Contains(err.Error(), "signups_have_exactly_one_channel") {
		t.Fatalf("err = %v, want a signups_have_exactly_one_channel violation", err)
	}
}

func TestSignupsRefuseNeitherChannel(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t).Pool()

	_, err := pool.Exec(ctx,
		`INSERT INTO signups (token_hash, expires_at)
		 VALUES ('\x01', now() + interval '1 hour')`)
	if err == nil {
		t.Fatal("insert with no channel succeeded, want constraint violation")
	}
	if !strings.Contains(err.Error(), "signups_have_exactly_one_channel") {
		t.Fatalf("err = %v, want a signups_have_exactly_one_channel violation", err)
	}
}

// TestConsumedLinkRequestsMustNameTheirChat pins the other half: a row cannot
// be marked consumed without recording which chat consumed it, because the
// per-chat rate limit counts exactly those rows.
func TestConsumedLinkRequestsMustNameTheirChat(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t).Pool()

	_, err := pool.Exec(ctx,
		`INSERT INTO telegram_link_requests (nonce_hash, expires_at, consumed_at)
		 VALUES ('\x02', now() + interval '1 hour', now())`)
	if err == nil {
		t.Fatal("consumed row with no chat_id succeeded, want constraint violation")
	}
	if !strings.Contains(err.Error(), "consumed_rows_name_their_chat") {
		t.Fatalf("err = %v, want a consumed_rows_name_their_chat violation", err)
	}
}
