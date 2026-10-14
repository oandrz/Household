package usecase_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// These tests send a burst of password guesses and hold every one of them
// at the same point before letting any finish. Without that barrier the
// guesses would run one after another and the limit would hold by accident
// (LEARNING pattern 19).

// burstSize is comfortably more than the policy allows, so "only the allowed
// few were checked" cannot pass by coincidence.
const burstSize = 10

// gatedHasher parks every Verify until release is closed, so a test decides
// when the checks finish. It is safe to call from many goroutines.
type gatedHasher struct {
	release chan struct{}

	mu      sync.Mutex
	parked  int
	against []string // the encoded hash each Verify was asked to check
}

func newGatedHasher() *gatedHasher { return &gatedHasher{release: make(chan struct{})} }

func (h *gatedHasher) Hash(_ context.Context, plain string) (string, error) {
	return "hashed:" + plain, nil
}

func (h *gatedHasher) Verify(_ context.Context, plain, encoded string) (bool, error) {
	h.mu.Lock()
	h.parked++
	h.against = append(h.against, encoded)
	h.mu.Unlock()
	<-h.release
	return encoded == "hashed:"+plain, nil
}

func (h *gatedHasher) Full() bool { return false }

func (h *gatedHasher) parkedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.parked
}

// checksAgainst counts the Verify calls made against one stored hash --
// the real checks, as opposed to the decoy ones.
func (h *gatedHasher) checksAgainst(encoded string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.against {
		if e == encoded {
			n++
		}
	}
	return n
}

// serialUsers and serialMembers put a lock around the fixture's doubles,
// which were written for one caller at a time.
type serialUsers struct {
	usecase.UserRepository
	mu *sync.Mutex
}

func (s serialUsers) ByEmail(ctx context.Context, email string) (usecase.StoredUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UserRepository.ByEmail(ctx, email)
}

func (s serialUsers) ByID(ctx context.Context, id string) (usecase.StoredUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UserRepository.ByID(ctx, id)
}

type serialMembers struct {
	usecase.MembershipRepository
	mu *sync.Mutex
}

func (s serialMembers) ByUser(ctx context.Context, userID string) (domain.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MembershipRepository.ByUser(ctx, userID)
}

// burstAuth is an AuthService over the fixture's household whose hasher the
// test controls.
func burstAuth(f *fixture, hasher usecase.PasswordHasher) *usecase.AuthService {
	var mu sync.Mutex
	return usecase.NewAuthService(usecase.AuthDeps{
		Users:      serialUsers{UserRepository: f.users, mu: &mu},
		Members:    serialMembers{MembershipRepository: f.members, mu: &mu},
		Sessions:   f.sessions,
		Attempts:   f.attempts,
		MagicLinks: f.magicLinks,
		Mailer:     f.mailer,
		Hasher:     hasher,
		Tokens:     &seqTokens{},
		Clock:      f.clock,
		SessionTTL: 30 * 24 * time.Hour,
		BaseURL:    "http://localhost:5173",
	})
}

// eventually polls until ok, and fails the test rather than hanging it.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// signInBurst sends burstSize wrong passwords for one address, waits until
// every one of them is parked inside the hasher, then lets them all finish.
// It returns each attempt's outcome.
func signInBurst(t *testing.T, auth *usecase.AuthService, hasher *gatedHasher, email string) []*usecase.SignInFailedError {
	t.Helper()
	outcomes := make([]*usecase.SignInFailedError, burstSize)
	var wg sync.WaitGroup
	for i := 0; i < burstSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := auth.SignIn(context.Background(), email, "a-wrong-guess")
			if !errors.As(err, &outcomes[i]) {
				t.Errorf("attempt %d: err = %v, want *SignInFailedError", i, err)
			}
		}()
	}
	eventually(t, "every attempt to reach the hasher", func() bool { return hasher.parkedCount() == burstSize })
	close(hasher.release)
	wg.Wait()
	return outcomes
}

// TestABurstOfGuessesGetsOnlyTheAllowedNumberOfRealChecks is the lockout's
// actual promise. Three tries means three: a burst that arrives all at once
// must not get one real password check per request.
func TestABurstOfGuessesGetsOnlyTheAllowedNumberOfRealChecks(t *testing.T) {
	f := newFixture(t)
	hasher := newGatedHasher()
	auth := burstAuth(f, hasher)
	andreas, err := f.users.ByEmail(context.Background(), "andreas@hearth.family")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}

	signInBurst(t, auth, hasher, "andreas@hearth.family")

	allowed := domain.DefaultLockoutPolicy().MaxAttempts
	if got := hasher.checksAgainst(andreas.PasswordHash); got != allowed {
		t.Fatalf("real password checks in a burst of %d = %d, want %d -- the policy's allowance", burstSize, got, allowed)
	}
	// Every guess stays on record, the ones that only met the decoy too:
	// an attempt made during a lock extends it.
	if got := f.attempts.failureCount(); got != burstSize {
		t.Fatalf("failures on record = %d, want %d", got, burstSize)
	}
}

// TestABurstAtAnUnknownAddressAnswersLikeABurstAtAMembers keeps the two
// indistinguishable under load as well as one request at a time: the same
// countdown, the same number of "locked" answers.
func TestABurstAtAnUnknownAddressAnswersLikeABurstAtAMembers(t *testing.T) {
	answers := func(email string) []string {
		f := newFixture(t)
		hasher := newGatedHasher()
		var out []string
		for _, o := range signInBurst(t, burstAuth(f, hasher), hasher, email) {
			if o == nil {
				continue
			}
			if o.Locked {
				out = append(out, "locked")
			} else {
				out = append(out, "tries left: "+string(rune('0'+o.AttemptsRemaining)))
			}
		}
		sort.Strings(out)
		return out
	}

	member, stranger := answers("andreas@hearth.family"), answers("nobody@example.com")
	if len(member) != burstSize || len(stranger) != burstSize {
		t.Fatalf("answers: member %d, stranger %d, want %d each", len(member), len(stranger), burstSize)
	}
	for i := range member {
		if member[i] != stranger[i] {
			t.Fatalf("a member's burst answered %v\nan unknown address's answered %v\nwant identical", member, stranger)
		}
	}
}

// TestABurstAtTheAdminPromptGetsOnlyTheAllowedNumberOfRealChecks is the same
// promise for the admin password prompt, which exists so that a stolen
// session cookie is not enough on its own.
func TestABurstAtTheAdminPromptGetsOnlyTheAllowedNumberOfRealChecks(t *testing.T) {
	users := newUserDouble()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()
	hasher := newGatedHasher()
	var mu sync.Mutex
	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: serialUsers{UserRepository: users, mu: &mu}, Attempts: attempts, Hasher: hasher,
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})

	var answered sync.WaitGroup
	var mine sync.Mutex
	finished, locked := 0, 0
	for i := 0; i < burstSize; i++ {
		answered.Add(1)
		go func() {
			defer answered.Done()
			err := svc.Verify(context.Background(), user.ID, "a-wrong-guess")
			mine.Lock()
			defer mine.Unlock()
			finished++
			if errors.Is(err, domain.ErrAdminLocked) {
				locked++
			}
		}()
	}
	// The barrier: every attempt has either been refused outright or is
	// parked inside the hasher. None that reached the hasher has finished.
	eventually(t, "every attempt to be refused or reach the hasher", func() bool {
		mine.Lock()
		defer mine.Unlock()
		return finished+hasher.parkedCount() == burstSize
	})
	close(hasher.release)
	answered.Wait()

	allowed := domain.DefaultLockoutPolicy().MaxAttempts
	if got := hasher.parkedCount(); got != allowed {
		t.Fatalf("password checks in a burst of %d = %d, want %d -- the policy's allowance", burstSize, got, allowed)
	}
	if want := burstSize - allowed; locked != want {
		t.Fatalf("attempts answered ErrAdminLocked = %d, want %d", locked, want)
	}
}

// busyHasher has room when asked, then answers "busy" at the door: the
// hasher that filled up between a service's early look and its real call.
type busyHasher struct{}

func (busyHasher) Full() bool { return false }

func (busyHasher) Hash(context.Context, string) (string, error) {
	return "", usecase.ErrHasherBusy
}
func (busyHasher) Verify(context.Context, string, string) (bool, error) {
	return false, usecase.ErrHasherBusy
}

// TestABusyHasherIsNeverCountedAsAWrongPassword: "busy" means nothing was
// checked. If it left a strike behind, a flood of requests could lock any
// household just by keeping the hasher full while its members sign in.
func TestABusyHasherIsNeverCountedAsAWrongPassword(t *testing.T) {
	for _, email := range []string{"andreas@hearth.family", "nobody@example.com"} {
		t.Run(email, func(t *testing.T) {
			f := newFixture(t)
			auth := burstAuth(f, busyHasher{})

			for i := 0; i < burstSize; i++ {
				_, err := auth.SignIn(context.Background(), email, "hunter2")
				if !errors.Is(err, usecase.ErrHasherBusy) {
					t.Fatalf("attempt %d: err = %v, want ErrHasherBusy", i, err)
				}
				var failure *usecase.SignInFailedError
				if errors.As(err, &failure) {
					t.Fatalf("attempt %d was reported as a failed sign-in: %+v", i, failure)
				}
			}
			if got := f.attempts.failureCount(); got != 0 {
				t.Fatalf("failures on record after %d busy answers = %d, want 0", burstSize, got)
			}

			// The household is not locked: the right password works the
			// moment the hasher has room again.
			if email == "andreas@hearth.family" {
				if _, err := f.auth.SignIn(context.Background(), email, "hunter2"); err != nil {
					t.Fatalf("sign-in after the busy spell: %v", err)
				}
			}
		})
	}
}

func TestABusyHasherIsNeverCountedAgainstTheAdminPrompt(t *testing.T) {
	users := newUserDouble()
	user := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	attempts := newFakeReauthAttemptRepo()
	svc := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: attempts, Hasher: busyHasher{},
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})

	for i := 0; i < burstSize; i++ {
		if err := svc.Verify(context.Background(), user.ID, "correct-horse"); !errors.Is(err, usecase.ErrHasherBusy) {
			t.Fatalf("attempt %d: err = %v, want ErrHasherBusy", i, err)
		}
	}
	if got := attempts.failureCount(user.ID); got != 0 {
		t.Fatalf("failures on record after %d busy answers = %d, want 0", burstSize, got)
	}
}

// fullHasher says it is full, and fails the test if it is used anyway.
type fullHasher struct{ t *testing.T }

func (fullHasher) Full() bool { return true }
func (h fullHasher) Hash(context.Context, string) (string, error) {
	h.t.Error("Hash was called on a hasher that said it was full")
	return "", usecase.ErrHasherBusy
}
func (h fullHasher) Verify(context.Context, string, string) (bool, error) {
	h.t.Error("Verify was called on a hasher that said it was full")
	return false, usecase.ErrHasherBusy
}

// TestAFullHasherIsRefusedBeforeAnyOtherWork: a request that is going to be
// told "busy" must be told before it has cost anything. If it looked up the
// user and wrote an attempt first, a flood of refused requests would queue
// for database connections, and everyone else would wait behind them.
func TestAFullHasherIsRefusedBeforeAnyOtherWork(t *testing.T) {
	f := newFixture(t)
	auth := burstAuth(f, fullHasher{t})
	lookupsBefore := f.users.byEmailCalls

	for _, email := range []string{"andreas@hearth.family", "nobody@example.com"} {
		if _, err := auth.SignIn(context.Background(), email, "hunter2"); !errors.Is(err, usecase.ErrHasherBusy) {
			t.Fatalf("%s: err = %v, want ErrHasherBusy", email, err)
		}
	}
	if got := f.users.byEmailCalls - lookupsBefore; got != 0 {
		t.Fatalf("user lookups made for refused sign-ins = %d, want 0", got)
	}
	if f.attempts.nextID != 0 {
		t.Fatalf("attempts reserved for refused sign-ins = %d, want 0", f.attempts.nextID)
	}

	users := newUserDouble()
	operator := users.mustCreate(t, "operator@example.test", "hashed:correct-horse", "Operator")
	ledger := newFakeReauthAttemptRepo()
	admin := usecase.NewAdminReauthService(usecase.AdminReauthDeps{
		Users: users, Attempts: ledger, Hasher: fullHasher{t},
		Clock: &fixedClock{now: time.Now()}, Policy: domain.DefaultLockoutPolicy(),
	})
	if err := admin.Verify(context.Background(), operator.ID, "correct-horse"); !errors.Is(err, usecase.ErrHasherBusy) {
		t.Fatalf("admin prompt: err = %v, want ErrHasherBusy", err)
	}
	if ledger.nextID != 0 {
		t.Fatalf("attempts reserved for a refused admin prompt = %d, want 0", ledger.nextID)
	}
}

// TestSettingAPasswordIsRefusedBeforeAnyOtherWorkWhenTheHasherIsFull is the
// same rule for the two routes that hash a new password. Every other
// dependency is left nil on purpose: touching one is the failure.
func TestSettingAPasswordIsRefusedBeforeAnyOtherWorkWhenTheHasherIsFull(t *testing.T) {
	signup := usecase.NewSignupService(usecase.SignupDeps{Hasher: fullHasher{t}})
	invite := usecase.NewInviteService(usecase.InviteDeps{Hasher: fullHasher{t}})

	calls := map[string]func() error{
		"completing a sign-up": func() error {
			_, err := signup.Complete(context.Background(), "a-token", "The Tans", "Mei", "SGD",
				"Asia/Singapore", "a long enough password")
			return err
		},
		"accepting an invite": func() error {
			_, err := invite.Accept(context.Background(), "a-token", "a long enough password", "Mei")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("did other work before refusing: %v", r)
				}
			}()
			if err := call(); !errors.Is(err, usecase.ErrHasherBusy) {
				t.Fatalf("err = %v, want ErrHasherBusy", err)
			}
		})
	}
}

// busyOnceHasher is busy for its first Hash and an ordinary fakeHasher after.
type busyOnceHasher struct {
	fakeHasher
	hashed int
}

func (h *busyOnceHasher) Hash(ctx context.Context, plain string) (string, error) {
	h.hashed++
	if h.hashed == 1 {
		return "", usecase.ErrHasherBusy
	}
	return h.fakeHasher.Hash(ctx, plain)
}

// TestABusyMomentDoesNotLeaveTheDecoyBroken: the decoy hash is made on
// first use. If "busy" at that moment were remembered, every later decoy
// check would be against a value the hasher rejects without doing any work,
// and an unknown address would answer faster than a real one for good.
func TestABusyMomentDoesNotLeaveTheDecoyBroken(t *testing.T) {
	f := newFixture(t)
	hasher := &busyOnceHasher{}
	auth := burstAuth(f, hasher)

	if _, err := auth.SignIn(context.Background(), "nobody@example.com", "x"); !errors.Is(err, usecase.ErrHasherBusy) {
		t.Fatalf("first sign-in: err = %v, want ErrHasherBusy", err)
	}

	var failure *usecase.SignInFailedError
	if _, err := auth.SignIn(context.Background(), "nobody@example.com", "x"); !errors.As(err, &failure) {
		t.Fatalf("second sign-in: err = %v, want *SignInFailedError", err)
	}
	const decoy = "hashed:decoy-password-for-timing-parity"
	if got := hasher.verifyCallsWithEncoded(decoy); got != 1 {
		t.Fatalf("decoy checks against a real hash = %d, want 1; the hasher saw %v", got, hasher.verifyLog)
	}
}

// TestTheDecoyPasswordSignsNobodyIn: the decoy's password is a constant in
// the source, and the decoy check does match it. That match must never be
// read as a correct password.
func TestTheDecoyPasswordSignsNobodyIn(t *testing.T) {
	const decoyPassword = "decoy-password-for-timing-parity"
	f := newFixture(t)

	// Ethan has no password, so his attempts are checked against the decoy.
	ethan, err := f.users.ByID(context.Background(), f.ethanID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	for _, email := range []string{ethan.Email, "nobody@example.com"} {
		var failure *usecase.SignInFailedError
		if _, err := f.auth.SignIn(context.Background(), email, decoyPassword); !errors.As(err, &failure) {
			t.Fatalf("%s with the decoy password: err = %v, want *SignInFailedError", email, err)
		}
	}
	if got := f.sessions.count(); got != 0 {
		t.Fatalf("sessions = %d, want 0", got)
	}
}
