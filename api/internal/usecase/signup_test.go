package usecase_test

import (
	"context"
	"errors"
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
				f.signups.setGlobalCount(1000)
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

	t.Run("registered address gets an existing-account notice with no token", func(t *testing.T) {
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
		// No signup row was written, so there is no token that could provision
		// a second household for this address.
		if f.signups.createCount() != 0 {
			t.Fatalf("wrote %d signup rows for a registered address, want 0", f.signups.createCount())
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
