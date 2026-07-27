// Command adminctl is Hearth's operational CLI: the seed that gives a clean
// checkout its bootstrap household, and the handful of support operations
// (resetting a password, unlocking a locked-out household, inviting a new
// member) that are genuinely operator actions and have no business behind an
// authenticated HTTP endpoint.
//
// It wires its own repositories and services exactly the way cmd/api/main.go
// does -- one implementation per port, built from the same config -- rather
// than inventing a second wiring style. The one thing it adds beyond that
// wiring is a capturingTokens wrapper (below) so create-invite can print the
// URL InviteService.Create only ever emails, never returns.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/andreasoentoro/hearth/api/internal/adapter/clock"
	"github.com/andreasoentoro/hearth/api/internal/adapter/crypto"
	"github.com/andreasoentoro/hearth/api/internal/adapter/mail"
	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/config"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "adminctl:", err)
		os.Exit(1)
	}
}

const usage = `usage: adminctl <command> [flags]

commands:
  seed                                          seed the design's household in development
  reset-password --email=<email>                set a member's password (read from stdin)
  unlock-household [--email=]                   clear a household's failed sign-in attempts (any
                                                 member's address; defaults to the seeded owner)
  create-invite --email= --name= --role=        invite a new member (role: owner or limited);
    [--capabilities=] [--inviter-email=]        the household is resolved from --inviter-email
                                                 (any member's address; defaults to the seeded owner)
  prune [--older-than=30]                       delete consumed/expired signups and login attempts
                                                 older than this many days (minimum 7)
`

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Both of seed's guards run before postgres.Open below, not after:
	// Open verifies the pool by pinging it, so checking afterwards would
	// mean a connection to the wrong database had already been attempted by
	// the time either guard had a say. Refusing first is the only way
	// "refuse to seed a production database" also means "never even talk to
	// one."
	if args[0] == "seed" {
		if !cfg.IsDevelopment() {
			return fmt.Errorf("refusing to seed outside development (APP_ENV=%s)", cfg.AppEnv)
		}
		if err := requireLocalDatabase(cfg.DatabaseURL); err != nil {
			return err
		}
	}

	ctx := context.Background()
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Repositories, one implementation per port, mirroring cmd/api/main.go's
	// wiring -- adminctl is a second entry point over the same production
	// stack, not a second architecture.
	users := postgres.NewUserRepo(db)
	households := postgres.NewHouseholdRepo(db)
	memberships := postgres.NewMembershipRepo(db)
	sessions := postgres.NewSessionRepo(db)
	loginAttempts := postgres.NewLoginAttemptRepo(db)
	invites := postgres.NewInviteRepo(db)
	spaces := postgres.NewSpaceRepo(db)
	notifications := postgres.NewNotificationRepo(db)

	hasher := crypto.NewArgon2Hasher(cfg.Argon2Time, cfg.Argon2MemoryKiB, cfg.Argon2Threads)
	tokens := crypto.NewTokenGenerator()
	sysClock := clock.System{}
	mailer := mail.NewSMTPMailer(cfg.SMTPAddr, cfg.SMTPFrom, cfg.AppBaseURL,
		cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPTLSMode)

	switch args[0] {
	case "seed":
		return runSeed(ctx, usecase.SeedDeps{
			Households:    households,
			Users:         users,
			Memberships:   memberships,
			Spaces:        spaces,
			Notifications: notifications,
			Invites:       invites,
			Mailer:        mailer,
			Hasher:        hasher,
			Tokens:        tokens,
			Clock:         sysClock,
			BaseURL:       cfg.AppBaseURL,
		})
	case "reset-password":
		return runResetPassword(ctx, args[1:], users, hasher, sessions)
	case "unlock-household":
		return runUnlockHousehold(ctx, args[1:], users, memberships, loginAttempts)
	case "create-invite":
		return runCreateInvite(ctx, args[1:], usecase.InviteDeps{
			Invites: invites,
			Users:   users,
			Mailer:  mailer,
			Hasher:  hasher,
			Tokens:  tokens,
			Clock:   sysClock,
			BaseURL: cfg.AppBaseURL,
		}, users, memberships)
	case "prune":
		fs := flag.NewFlagSet("prune", flag.ContinueOnError)
		fs.SetOutput(io.Discard) // see the matching comment in runResetPassword
		days := fs.Int("older-than", 30, "delete consumed/expired rows older than this many days")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runPrune(ctx, postgres.NewSignupRepo(db), loginAttempts,
			time.Duration(*days)*24*time.Hour)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// runSeed is the only subcommand gated on the environment and the database
// host -- both checked in run, above, before the connection those guards are
// meant to prevent is ever opened.
func runSeed(ctx context.Context, deps usecase.SeedDeps) error {
	result, err := usecase.Seed(ctx, deps)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	fmt.Println("Seeded the household \"Andreas & Christine\".")
	fmt.Printf("  Andreas:            %s / %s\n", usecase.AndreasEmail, usecase.DevPassword)
	switch {
	case result.ChristineIsMember:
		fmt.Println("  Christine has already accepted her invite; she is a member of the household.")
	case result.InviteURL != "":
		fmt.Printf("  Christine's invite: %s\n", result.InviteURL)
	default:
		fmt.Println("  Christine already has an invite pending that this seed did not create; " +
			"check Mailpit for the link.")
	}
	return nil
}

// requireLocalDatabase is Seed's second, independent guard. APP_ENV and
// DATABASE_URL are set separately: APP_ENV=development alone says nothing
// about which database is actually about to receive a known password and a
// known invite token, so the environment guard above is honestly the "only
// thing standing between Seed and a production database" (see DevPassword's
// doc comment in seed.go) only for as long as nothing else can point Seed at
// one. This closes that gap by naming the target: two independent
// conditions must now hold before Seed runs, and this is the one that
// actually inspects which database it is about to write to.
func requireLocalDatabase(databaseURL string) error {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("refusing to seed: could not parse DATABASE_URL: %w", err)
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1", "postgres":
		return nil
	default:
		return fmt.Errorf(
			"refusing to seed: DATABASE_URL host %q is not a recognised local address "+
				"(want localhost, 127.0.0.1, ::1 or postgres)", host)
	}
}

// runResetPassword revokes every one of the user's live sessions once the
// new password is set. A password reset is precisely the moment an account
// may be compromised -- that is the whole reason an operator is doing this
// by hand rather than the member using their own "forgot password" flow --
// and an attacker's existing session surviving the reset would defeat the
// point of it: the operator sees "password reset for x@y.com" and believes
// the account is secured, while a session minted before the reset keeps
// working exactly as it did before.
func runResetPassword(ctx context.Context, args []string, users usecase.UserRepository, hasher usecase.PasswordHasher,
	sessions usecase.SessionRepository) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	// ContinueOnError already writes its own error and usage to stderr by
	// default; discarding that here avoids printing the same failure twice,
	// once from flag and once from run's own "adminctl: %v" wrapper.
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "the member's email address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("--email is required")
	}

	user, err := users.ByEmail(ctx, *email)
	if err != nil {
		return fmt.Errorf("look up %q: %w", *email, err)
	}

	// Read from the terminal, not a flag: a password passed as a
	// command-line argument would sit in shell history and in this
	// process's argv for as long as it runs (visible to anyone who can read
	// /proc or run ps). term.ReadPassword also puts the terminal in raw
	// mode for the read, so the password is never echoed back either.
	password, err := readPassword("New password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	if password == "" {
		return errors.New("password must not be empty")
	}

	hash, err := hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := users.SetPasswordHash(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("set password hash: %w", err)
	}

	if err := sessions.RevokeAllForUser(ctx, user.ID); err != nil {
		return fmt.Errorf("password was reset, but revoking existing sessions failed: %w", err)
	}

	fmt.Printf("Password reset for %s. Their existing sessions have been revoked.\n", *email)
	return nil
}

// readPassword prompts on stderr (so stdout stays script-friendly) and reads
// one line from the controlling terminal without echoing it.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// runUnlockHousehold resolves the household through --email, any member's
// address -- see resolveHouseholdByEmail for why that replaced resolving
// "the household" through the seeded owner alone.
func runUnlockHousehold(ctx context.Context, args []string, users usecase.UserRepository,
	memberships usecase.MembershipRepository, attempts usecase.LoginAttemptRepository) error {
	fs := flag.NewFlagSet("unlock-household", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // see the matching comment in runResetPassword
	email := fs.String("email", usecase.AndreasEmail,
		"any member's address in the household to act on; defaults to the seeded owner")
	if err := fs.Parse(args); err != nil {
		return err
	}

	householdID, _, err := resolveHouseholdByEmail(ctx, users, memberships, *email, "email")
	if err != nil {
		return err
	}

	if err := attempts.ClearFailures(ctx, householdID); err != nil {
		return fmt.Errorf("clear failures: %w", err)
	}

	fmt.Println("Household unlocked.")
	return nil
}

func runCreateInvite(ctx context.Context, args []string, deps usecase.InviteDeps,
	users usecase.UserRepository, memberships usecase.MembershipRepository) error {
	fs := flag.NewFlagSet("create-invite", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // see the matching comment in runResetPassword
	email := fs.String("email", "", "the invitee's email address")
	name := fs.String("name", "", "the invitee's display name")
	roleFlag := fs.String("role", "", "owner or limited")
	capsFlag := fs.String("capabilities", "", "comma-separated: calendar,chores,money,marriage")
	inviterEmail := fs.String("inviter-email", usecase.AndreasEmail,
		"any member's address in the household to invite into; defaults to the seeded owner")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" || *name == "" || *roleFlag == "" {
		return errors.New("--email, --name and --role are required")
	}

	role, err := domain.ParseRole(*roleFlag)
	if err != nil {
		return err
	}

	var capValues []string
	if *capsFlag != "" {
		capValues = strings.Split(*capsFlag, ",")
	}
	// No default expansion for role=owner here: domain.NewMembership (via
	// InviteService.Create) is the single enforcement point for "an owner
	// must hold every capability", and it must be free to reject an
	// incomplete --capabilities list rather than have this CLI silently
	// widen an operator's typed input into a bigger grant than they asked
	// for.
	caps, err := domain.ParseCapabilities(capValues)
	if err != nil {
		return err
	}

	householdID, inviterID, err := resolveHouseholdByEmail(ctx, users, memberships, *inviterEmail, "inviter-email")
	if err != nil {
		return err
	}

	// InviteService.Create only ever emails the URL it builds; it returns
	// nothing describing what was sent. Wrapping Tokens captures the raw
	// value Create actually minted so it can be printed here too.
	captured := &capturingTokens{inner: deps.Tokens}
	deps.Tokens = captured
	inviteSvc := usecase.NewInviteService(deps)

	if err := inviteSvc.Create(ctx, householdID, inviterID, *name, *email, role, caps); err != nil {
		return fmt.Errorf("create invite: %w", err)
	}

	fmt.Printf("Invited %s <%s> as %s: %s/invite/%s\n", *name, *email, role, deps.BaseURL, captured.lastRaw)
	return nil
}

// resolveHouseholdByEmail finds the household the given address belongs to,
// along with that user's id. It replaces a version that resolved "the
// household" through usecase.AndreasEmail, which was correct only while there
// was exactly one household per deployment -- self-serve sign-up ended that,
// and an operator unlocking the wrong customer's household is a worse failure
// than having to type an address.
//
// AndreasEmail remains the default in development so `make unlock-household`
// keeps working with no arguments against a seeded database.
//
// flagName is the caller's flag for this address (e.g. "email" for
// unlock-household, "inviter-email" for create-invite, which already has its
// own --email for the invitee) -- it names the right flag in the error below
// rather than pointing at one that means something else in that command.
func resolveHouseholdByEmail(ctx context.Context, users usecase.UserRepository,
	memberships usecase.MembershipRepository, email, flagName string) (householdID, userID string, err error) {
	user, err := users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", fmt.Errorf("no account for %q; pass --%s with a member's address", email, flagName)
		}
		return "", "", fmt.Errorf("look up %q: %w", email, err)
	}
	membership, err := memberships.ByUser(ctx, user.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", fmt.Errorf("%q has an account but belongs to no household", email)
		}
		return "", "", fmt.Errorf("resolve the household for %q: %w", email, err)
	}
	return membership.HouseholdID, user.ID, nil
}

// pruneFloor is the shortest retention window `prune` will accept. It is far
// outside domain.LockoutPolicy.Window (15 minutes), because deleting a
// login_attempts row still inside that window would clear a live lockout --
// which would turn a cleanup command into a way to unlock a household that is
// actively being guessed at. The floor is enforced rather than documented so
// nobody can reach that state with a plausible-looking flag value.
const pruneFloor = 7 * 24 * time.Hour

// runPrune deletes consumed/expired signups and login attempts older than
// olderThan. See pruneFloor for why a window under seven days is refused
// rather than merely discouraged.
func runPrune(ctx context.Context, signups usecase.SignupRepository,
	attempts usecase.LoginAttemptRepository, olderThan time.Duration) error {
	if olderThan < pruneFloor {
		return fmt.Errorf("--older-than must be at least %d days: pruning login attempts inside the "+
			"lockout window would clear a live lockout", int(pruneFloor.Hours()/24))
	}
	before := time.Now().Add(-olderThan)

	prunedSignups, err := signups.Prune(ctx, before)
	if err != nil {
		return fmt.Errorf("prune signups: %w", err)
	}
	// login_attempts is pruned here rather than in its own command because
	// ClearFailures -- the only thing that ever deleted from it -- is scoped
	// WHERE household_id = $1, which never matches the NULL rows an
	// unknown-address sign-in attempt records. Those rows were deleted by
	// nothing at all, and a public sign-in endpoint means a stranger can create
	// them without limit.
	prunedAttempts, err := attempts.Prune(ctx, before)
	if err != nil {
		return fmt.Errorf("prune login attempts: %w", err)
	}

	fmt.Printf("Pruned %d signups and %d login attempts older than %s.\n",
		prunedSignups, prunedAttempts, before.Format(time.RFC3339))
	return nil
}

// capturingTokens wraps the real TokenGenerator, recording the raw value of
// the last token it minted. lastRaw is set only once NewToken has actually
// succeeded, so a caller never prints a token for a mint that failed.
type capturingTokens struct {
	inner   usecase.TokenGenerator
	lastRaw string
}

func (c *capturingTokens) NewToken() (string, []byte, error) {
	raw, hash, err := c.inner.NewToken()
	if err != nil {
		return "", nil, err
	}
	c.lastRaw = raw
	return raw, hash, nil
}

func (c *capturingTokens) HashToken(raw string) []byte { return c.inner.HashToken(raw) }
