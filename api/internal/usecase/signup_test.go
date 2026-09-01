package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// wantReadSequence is the order every branch of Request must produce. It is
// asserted as an exact sequence, not a set: the point is that no branch skips a
// read and no branch reorders them.
var wantReadSequence = []string{
	"Signups.CountSince",
	"Signups.CountForEmailSince",
	"Users.ByEmail",
}

func TestSignupRequestIsIndistinguishableAcrossBranches(t *testing.T) {
	type branch struct {
		name  string
		email string
		setUp func(t *testing.T, f *signupFixture)
	}

	branches := []branch{
		{
			name:  "fresh address",
			email: "fresh@example.test",
			setUp: func(*testing.T, *signupFixture) {},
		},
		{
			name:  "address that already has an account",
			email: "taken@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.users.put(usecase.StoredUser{
					User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
					PasswordHash: "hashed:whatever",
				})
			},
		},
		{
			name:  "address at its hourly limit",
			email: "busy@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.setEmailCount("busy@example.test", 3)
			},
		},
		{
			name:  "global daily mail ceiling reached",
			email: "unlucky@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.setGlobalCount(usecase.SignupGlobalDailyLimit)
			},
		},
		{
			name:  "mailer is down",
			email: "relay@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.mailer.failEverySend(errors.New("relay refused"))
			},
		},
		{
			name:  "token generation fails",
			email: "entropy@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.tokens.failNext(errors.New("entropy exhausted"))
			},
		},
		{
			name:  "the signup insert fails",
			email: "dbdown@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.signups.failNextCreate(errors.New("statement timeout"))
			},
		},
		{
			// Sibling of "the signup insert fails" above, for the write the
			// fix round added: a registered address's branch now writes a row
			// too (CreateConsumed, not Create), and that write can fail in
			// exactly the same way.
			name:  "the signup insert fails for a registered address",
			email: "dbdown2@example.test",
			setUp: func(t *testing.T, f *signupFixture) {
				f.users.put(usecase.StoredUser{
					User:         domain.User{ID: "u2", Email: "dbdown2@example.test", DisplayName: "Ade"},
					PasswordHash: "hashed:whatever",
				})
				f.signups.failNextCreate(errors.New("statement timeout"))
			},
		},
	}

	for _, b := range branches {
		t.Run(b.name, func(t *testing.T) {
			f := newSignupFixture(t)
			b.setUp(t, f)

			// Every branch returns nil. Not "returns nil unless the mailer
			// broke" -- an error on one branch only is a discrete yes/no oracle,
			// cheaper to exploit than any timing measurement.
			if err := f.svc.Request(context.Background(), b.email); err != nil {
				t.Fatalf("Request returned %v, want nil", err)
			}

			got := f.log.seq()
			if len(got) != len(wantReadSequence) {
				t.Fatalf("read sequence = %v, want %v", got, wantReadSequence)
			}
			for i := range wantReadSequence {
				if got[i] != wantReadSequence[i] {
					t.Fatalf("read sequence = %v, want %v", got, wantReadSequence)
				}
			}
		})
	}
}

// TestSignupRequestRejectsImplausibleEmailBeforeAnyWork is change 5's own
// test: without a server-side shape check, POST /auth/sign-up
// {"email":""} writes a countable signups row and, worse, does it for free --
// no counting read stands in the way, so it costs nothing to burn the global
// daily ceiling this way. isPlausibleEmail must refuse it before any of that:
// no row, no mail, and (the part row-count and mail alone cannot prove) no
// counting read either -- len(f.log.seq()) == 0 is the assertion that matches
// "before any counting read", not just "before any row write".
func TestSignupRequestRejectsImplausibleEmailBeforeAnyWork(t *testing.T) {
	for _, email := range []string{
		"",
		"   ",
		"not-an-email",
		"@example.test",
		"person@",
		"person@example",
		"has space@example.test",
	} {
		t.Run(email, func(t *testing.T) {
			f := newSignupFixture(t)
			if err := f.svc.Request(context.Background(), email); err != nil {
				t.Fatalf("Request(%q) = %v, want nil", email, err)
			}
			f.mailer.assertNoSendsWithin(t, 100*time.Millisecond)
			if n := f.signups.createCount(); n != 0 {
				t.Fatalf("wrote %d signups rows for %q, want 0", n, email)
			}
			if got := f.log.seq(); len(got) != 0 {
				t.Fatalf("read sequence for %q = %v, want none -- "+
					"a malformed address must be refused before any counting read", email, got)
			}
		})
	}
}

// The registered branch must send mail too. If only the fresh branch did, the
// absence of an email would tell anyone who can read the mailbox that the
// address is registered -- the oracle the identical return value exists to
// prevent.
func TestSignupRequestMailsBothBranches(t *testing.T) {
	t.Run("fresh address gets a create-household link", func(t *testing.T) {
		f := newSignupFixture(t)
		if err := f.svc.Request(context.Background(), "fresh@example.test"); err != nil {
			t.Fatalf("Request: %v", err)
		}
		f.mailer.waitForSends(t, 1)
		if n := len(f.mailer.signupLinks); n != 1 {
			t.Fatalf("sent %d signup links, want 1", n)
		}
		if n := len(f.mailer.existingAccountNotices); n != 0 {
			t.Fatalf("sent %d existing-account notices, want 0", n)
		}
		if !strings.Contains(f.mailer.signupLinks[0].URL, "/sign-up/") {
			t.Fatalf("link url = %q, want it to contain /sign-up/", f.mailer.signupLinks[0].URL)
		}
	})

	t.Run("registered address gets an existing-account notice with no usable token", func(t *testing.T) {
		f := newSignupFixture(t)
		f.users.put(usecase.StoredUser{
			User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
			PasswordHash: "hashed:whatever",
		})
		if err := f.svc.Request(context.Background(), "taken@example.test"); err != nil {
			t.Fatalf("Request: %v", err)
		}
		f.mailer.waitForSends(t, 1)
		if n := len(f.mailer.existingAccountNotices); n != 1 {
			t.Fatalf("sent %d existing-account notices, want 1", n)
		}
		if n := len(f.mailer.signupLinks); n != 0 {
			t.Fatalf("sent %d signup links to a registered address, want 0", n)
		}
		// A signup row IS written here (fix round: this is what makes
		// CountForEmailSince/CountSince advance for this branch, the same way
		// they advance for a fresh one -- see Request's doc comment). But the
		// row is written already-consumed, via CreateConsumed rather than
		// Create, so it can never provision a second household for this
		// address: Provision's guarded UPDATE (ConsumeSignup) requires
		// consumed_at IS NULL.
		if got := f.signups.createCount(); got != 1 {
			t.Fatalf("wrote %d signup rows for a registered address, want 1 (a pre-consumed counter row)", got)
		}
		var consumed bool
		for _, row := range f.signups.rows {
			consumed = row.ConsumedAt != nil
		}
		if !consumed {
			t.Fatal("the row written for a registered address must be pre-consumed, not a live token")
		}
	})

	t.Run("a rate-limited address gets no mail at all", func(t *testing.T) {
		// Both branches are gated by the limit, deliberately: if only the fresh
		// branch were, someone could flood a registered address's inbox.
		for _, tc := range []struct {
			name  string
			email string
			setUp func(f *signupFixture)
		}{
			{"fresh but limited", "fresh@example.test", func(f *signupFixture) {
				f.signups.setEmailCount("fresh@example.test", 3)
			}},
			{"registered and limited", "taken@example.test", func(f *signupFixture) {
				f.users.put(usecase.StoredUser{
					User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
					PasswordHash: "hashed:whatever",
				})
				f.signups.setEmailCount("taken@example.test", 3)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newSignupFixture(t)
				tc.setUp(f)
				if err := f.svc.Request(context.Background(), tc.email); err != nil {
					t.Fatalf("Request: %v", err)
				}
				f.mailer.assertNoSendsWithin(t, 100*time.Millisecond)
			})
		}
	})
}

// TestSignupRequestRateLimitAppliesThroughRealCounters is the fix-round test:
// it drives the per-address limit through the real CountForEmailSince path --
// repeated calls to Request -- rather than through setEmailCount, which only
// proves the shared `if` has the right shape, not that the counter it reads
// is ever populated on both branches. Before the fix, CountForEmailSince
// never saw a row for a registered address (nothing ever wrote one), so its
// count stayed at zero forever and the registered subtest below would send an
// existing-account notice on every one of the four calls instead of exactly
// three.
func TestSignupRequestRateLimitAppliesThroughRealCounters(t *testing.T) {
	t.Run("a registered address gets no more than three existing-account notices", func(t *testing.T) {
		f := newSignupFixture(t)
		f.users.put(usecase.StoredUser{
			User:         domain.User{ID: "u1", Email: "taken@example.test", DisplayName: "Ade"},
			PasswordHash: "hashed:whatever",
		})

		for i := 0; i < 4; i++ {
			if err := f.svc.Request(context.Background(), "taken@example.test"); err != nil {
				t.Fatalf("Request #%d: %v", i+1, err)
			}
		}
		f.mailer.waitForSends(t, 3)
		// waitForSends(t, 3) only proves 3 sends landed -- it drains exactly
		// that many and returns, so a 4th async send in flight would not yet
		// have arrived and this test would pass whether or not the 4th
		// request was actually rate-limited. This is the negative half of the
		// assertion: nothing more arrives in the time a 4th send would need.
		f.mailer.assertNoSendsWithin(t, 100*time.Millisecond)
		if n := len(f.mailer.existingAccountNotices); n != 3 {
			t.Fatalf("sent %d existing-account notices for 4 requests, want 3 -- "+
				"the 4th call must be silently rate-limited, not mailed", n)
		}
	})

	t.Run("a fresh address gets no more than three sign-up links", func(t *testing.T) {
		f := newSignupFixture(t)

		for i := 0; i < 4; i++ {
			if err := f.svc.Request(context.Background(), "fresh@example.test"); err != nil {
				t.Fatalf("Request #%d: %v", i+1, err)
			}
		}
		f.mailer.waitForSends(t, 3)
		// Same reasoning as the registered-address subtest above: draining 3
		// signals does not rule out a 4th arriving moments later, so a 4th
		// send must be actively ruled out, not merely un-observed.
		f.mailer.assertNoSendsWithin(t, 100*time.Millisecond)
		if n := len(f.mailer.signupLinks); n != 3 {
			t.Fatalf("sent %d signup links for 4 requests, want 3", n)
		}
	})
}

// TestSignupRequestGlobalCeilingResetsAtMidnight is change 3's own test. The
// old window was now.Add(-24*time.Hour): rolling, recovering an hour at a
// time, with no answer to "when does this clear" other than "wait and see".
// The spec (design doc section 8) calls for the count to reset at midnight
// instead, which this proves by exploiting the one place the two windows
// actually disagree: a row written late the day before is outside a
// calendar-day window measured from shortly after midnight, but still inside
// a rolling 24-hour window measured from that same instant.
func TestSignupRequestGlobalCeilingResetsAtMidnight(t *testing.T) {
	f := newSignupFixture(t)

	// 23:00 the day before "today". Every filler row below is created at this
	// instant, filling the global ceiling entirely with yesterday's traffic.
	yesterday := time.Date(2026, 7, 19, 23, 0, 0, 0, time.UTC)
	f.clock.now = yesterday
	for i := 0; i < usecase.SignupGlobalDailyLimit; i++ {
		hash := fmt.Sprintf("midnight-filler-hash-%d", i)
		f.signups.rows[hash] = &signupRow{
			ID:        fmt.Sprintf("midnight-filler-%d", i),
			Email:     fmt.Sprintf("midnight-filler-%d@example.test", i),
			CreatedAt: yesterday,
			ExpiresAt: yesterday.Add(usecase.SignupTTL),
		}
	}

	// Move 90 minutes forward, across midnight into today.
	f.clock.Advance(90 * time.Minute)

	// Under the old rolling-24-hour window, "since" here is yesterday 00:30 --
	// comfortably before every filler row's 23:00 timestamp, so all
	// SignupGlobalDailyLimit of them would still count and this request would
	// be silently declined: no error, but no mail either. Under a
	// calendar-day window, "since" is today's 00:00, strictly after every
	// filler row's timestamp, so none of them count towards today and this
	// request must succeed.
	if err := f.svc.Request(context.Background(), "today@example.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	f.mailer.waitForSends(t, 1)
	if n := len(f.mailer.signupLinks); n != 1 {
		t.Fatalf("sent %d signup links, want 1 -- the global ceiling must reset at midnight, "+
			"not stay a rolling 24-hour window", n)
	}
}

func TestSignupPreview(t *testing.T) {
	t.Run("returns the address a live token was issued for", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		got, err := f.svc.Preview(context.Background(), token)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if got.Email != "founder@example.test" {
			t.Fatalf("Email = %q, want founder@example.test", got.Email)
		}
		if got.Channel != "email" {
			t.Fatalf("Channel = %q, want email", got.Channel)
		}
	})

	t.Run("reports the Telegram channel for a Telegram sign-up", func(t *testing.T) {
		f := newSignupFixture(t)
		chatID := int64(4242)
		f.signups.rows[string(f.tokens.HashToken("token"))] = &signupRow{
			ID:             "signup-telegram",
			TelegramChatID: &chatID,
			CreatedAt:      f.clock.Now(),
			ExpiresAt:      f.clock.Now().Add(time.Hour),
		}

		preview, err := f.svc.Preview(context.Background(), "token")
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if preview.Channel != "telegram" {
			t.Fatalf("Channel = %q, want telegram", preview.Channel)
		}
		if preview.Email != "" {
			t.Fatalf("Email = %q, want empty for a Telegram sign-up", preview.Email)
		}
	})

	// signupChannel's fail-closed default branch. The
	// signups_have_exactly_one_channel constraint should make this row
	// unreachable in production; this is the second gate, for whatever
	// bypasses the database -- Preview must refuse it, not guess a channel.
	t.Run("a row naming no channel is refused, not guessed at", func(t *testing.T) {
		f := newSignupFixture(t)
		f.signups.rows[string(f.tokens.HashToken("token"))] = &signupRow{
			ID:        "signup-channelless",
			CreatedAt: f.clock.Now(),
			ExpiresAt: f.clock.Now().Add(time.Hour),
		}
		if _, err := f.svc.Preview(context.Background(), "token"); err == nil {
			t.Fatal("Preview accepted a signup naming neither channel, want an error")
		}
	})

	t.Run("an expired token reports ErrTokenExpired", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "late@example.test", f.clock.Now().Add(-time.Minute))
		if _, err := f.svc.Preview(context.Background(), token); !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
		}
	})

	t.Run("a consumed token reports ErrSignupAlreadyUsed, not expiry", func(t *testing.T) {
		// The distinction is the point: "you already used this, sign in" and
		// "this lapsed, start again" need different answers.
		f := newSignupFixture(t)
		token := f.issueSignup(t, "done@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.markConsumed(f.tokens.HashToken(token), f.clock.Now())
		if _, err := f.svc.Preview(context.Background(), token); !errors.Is(err, usecase.ErrSignupAlreadyUsed) {
			t.Fatalf("error = %v, want usecase.ErrSignupAlreadyUsed", err)
		}
	})

	t.Run("an unknown token reports ErrNotFound", func(t *testing.T) {
		f := newSignupFixture(t)
		if _, err := f.svc.Preview(context.Background(), "never-issued"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("error = %v, want domain.ErrNotFound", err)
		}
	})
}

func TestSignupComplete(t *testing.T) {
	t.Run("provisions and issues a session", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))

		got, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password")
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got.SessionToken == "" {
			t.Fatal("SessionToken is empty")
		}
		if got.UserID == "" || got.HouseholdID == "" {
			t.Fatalf("got %+v, want UserID and HouseholdID populated", got)
		}
		// Issued through the same issueSession sign-in uses, so the expiry
		// schedule is identical.
		if want := f.clock.Now().Add(f.sessionTTL); !got.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
		}
		if f.sessions.count() != 1 {
			t.Fatalf("created %d sessions, want 1", f.sessions.count())
		}
	})

	t.Run("the password is hashed, never stored raw", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got := f.signups.lastProvisionPasswordHash(); got != "hashed:a-long-enough-password" {
			t.Fatalf("password hash = %q, want the hasher's output", got)
		}
	})

	t.Run("a short password is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "short"); !errors.Is(err, usecase.ErrPasswordTooShort) {
			t.Fatalf("error = %v, want ErrPasswordTooShort", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected password")
		}
	})

	t.Run("a blank household name is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "   ", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, usecase.ErrHouseholdNameRequired) {
			t.Fatalf("error = %v, want ErrHouseholdNameRequired", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected household name")
		}
	})

	t.Run("an unknown currency is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "ZZZ", "a-long-enough-password"); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Fatalf("error = %v, want domain.ErrInvalidMoney", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a rejected currency")
		}
	})

	t.Run("a currency Money cannot render is refused before anything is written", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "founder@example.test", f.clock.Now().Add(usecase.SignupTTL))
		// JPY is a well-formed, active ISO 4217 code -- domain.ParseCurrency
		// alone would accept it -- but it has zero minor units, and
		// domain.Money.String() hard-codes two decimal places. Sign-up must
		// refuse it through the same domain.SelectableCurrencies gate GET
		// /api/v1/currencies filters through, or a client posting directly
		// (bypassing the form's own currency list) provisions a household
		// every amount renders 100x wrong.
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "JPY", "a-long-enough-password"); !errors.Is(err, domain.ErrInvalidMoney) {
			t.Fatalf("error = %v, want domain.ErrInvalidMoney", err)
		}
		if f.signups.provisionCalls() != 0 {
			t.Fatal("Provision was called for a currency Money cannot render")
		}
	})

	t.Run("a consumed token is refused with ErrSignupAlreadyUsed", func(t *testing.T) {
		f := newSignupFixture(t)
		token := f.issueSignup(t, "done@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.markConsumed(f.tokens.HashToken(token), f.clock.Now())
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, usecase.ErrSignupAlreadyUsed) {
			t.Fatalf("error = %v, want ErrSignupAlreadyUsed", err)
		}
	})

	t.Run("Provision's own refusal is returned as-is", func(t *testing.T) {
		// Provision's guarded UPDATE is authoritative for the race between the
		// TokenLifecycle read above and the write. Its answer must not be
		// re-derived from the stale read.
		f := newSignupFixture(t)
		token := f.issueSignup(t, "racer@example.test", f.clock.Now().Add(usecase.SignupTTL))
		f.signups.failNextProvision(domain.ErrTokenExpired)
		if _, err := f.svc.Complete(context.Background(), token, "Ade & Kris", "Ade", "SGD", "a-long-enough-password"); !errors.Is(err, domain.ErrTokenExpired) {
			t.Fatalf("error = %v, want domain.ErrTokenExpired", err)
		}
		if f.sessions.count() != 0 {
			t.Fatal("a session was issued for a failed provision")
		}
	})
}

// --- signupFixture ------------------------------------------------------

// signupFixture builds a SignupService over its own set of in-memory doubles,
// separate from the big fixture auth_test.go and invite_test.go share.
// SignupService.Complete needs a household/space/notification double chain
// underneath signupDouble.Provision that no other service in this package
// touches, so giving sign-up tests their own fixture keeps that wiring out of
// every unrelated test file rather than growing newFixture for one caller.
type signupFixture struct {
	svc        *usecase.SignupService
	users      *userDouble
	signups    *signupDouble
	sessions   *sessionDouble
	mailer     *mailerDouble
	tokens     *seqTokens
	clock      *fixedClock
	log        *readLog
	sessionTTL time.Duration
	baseURL    string
}

func newSignupFixture(t *testing.T) *signupFixture {
	t.Helper()

	clock := &fixedClock{now: time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)}
	log := &readLog{}

	users := newUserDouble()
	users.log = log
	members := newMembershipDouble(users)
	users.setMembers(members)

	households := newHouseholdDouble()
	spaces := newSpaceDouble()
	notifications := newNotificationDouble()

	signups := newSignupDouble(clock, households, users, members, spaces, notifications)
	signups.log = log

	sessions := newSessionDouble(clock)
	mailer := newMailerDouble()
	tokens := &seqTokens{}
	sessionTTL := 30 * 24 * time.Hour
	baseURL := "http://localhost:5173"

	svc := usecase.NewSignupService(usecase.SignupDeps{
		Signups:    signups,
		Users:      users,
		Sessions:   sessions,
		Mailer:     mailer,
		Hasher:     &fakeHasher{},
		Tokens:     tokens,
		Clock:      clock,
		SessionTTL: sessionTTL,
		BaseURL:    baseURL,
	})

	return &signupFixture{
		svc: svc, users: users, signups: signups, sessions: sessions,
		mailer: mailer, tokens: tokens, clock: clock, log: log,
		sessionTTL: sessionTTL, baseURL: baseURL,
	}
}

// issueSignup calls Request for email, waits for its async send to land, and
// returns the raw token the token double handed out -- so a test never has to
// know how the token was generated. It then overwrites the stored row's
// expiry directly (signups.rows is reachable from this file: both are in
// package usecase_test), because Request always mints a token with
// SignupTTL's fixed 24-hour expiry, and several Preview/Complete tests need an
// already-expired or soon-to-expire token instead.
func (f *signupFixture) issueSignup(t *testing.T, email string, expiresAt time.Time) string {
	t.Helper()
	if err := f.svc.Request(context.Background(), email); err != nil {
		t.Fatalf("issueSignup: Request: %v", err)
	}
	f.mailer.waitForSends(t, 1)

	sent := f.mailer.signupLinks[len(f.mailer.signupLinks)-1]
	raw := sent.URL[strings.LastIndex(sent.URL, "/")+1:]

	row, ok := f.signups.rows[string(f.tokens.HashToken(raw))]
	if !ok {
		t.Fatalf("issueSignup: no signup row found for token %q", raw)
	}
	row.ExpiresAt = expiresAt
	return raw
}
