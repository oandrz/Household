package config_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/config"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@localhost:5432/hearth?sslmode=disable")
	t.Setenv("SMTP_ADDR", "localhost:1025")
	t.Setenv("SMTP_FROM", "Hearth <noreply@hearth.localhost>")
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
}

func TestLoadReadsEnvironment(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "development" || cfg.Port != 8080 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.SMTPAddr != "localhost:1025" || cfg.SMTPFrom != "Hearth <noreply@hearth.localhost>" || cfg.AppBaseURL != "http://localhost:5173" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Argon2Time != 3 || cfg.Argon2MemoryKiB != 65536 || cfg.Argon2Threads != 2 {
		t.Fatalf("unexpected argon2 defaults: %+v", cfg)
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is empty")
	}
}

func TestLoadRejectsMissingAppEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when APP_ENV is unset")
	}
}

func TestLoadRejectsUnknownAppEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "staging")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for an unknown APP_ENV")
	}
}

func TestLoadRejectsOutOfRangePort(t *testing.T) {
	for _, port := range []string{"0", "-1", "70000"} {
		t.Run(port, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("PORT", port)

			if _, err := config.Load(); err == nil {
				t.Fatalf("expected an error for PORT=%s", port)
			}
		})
	}
}

func TestLoadAcceptsBoundaryPorts(t *testing.T) {
	for _, port := range []string{"1", "65535"} {
		t.Run(port, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("PORT", port)

			if _, err := config.Load(); err != nil {
				t.Fatalf("unexpected error for PORT=%s: %v", port, err)
			}
		})
	}
}

func TestLoadRejectsMissingSMTPFrom(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_FROM", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when SMTP_FROM is empty")
	}
}

// TestLoadDefaultsSMTPTLSModeToNoneInDevelopmentAndMandatoryOtherwise pins
// the production-blocker fix: the mailer used to hardcode NoTLS
// unconditionally, which no hosted relay accepts. The default must not break
// Mailpit (plain SMTP, development) while still refusing to send unencrypted
// by default everywhere else.
func TestLoadDefaultsSMTPTLSModeToNoneInDevelopmentAndMandatoryOtherwise(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMTPTLSMode != "none" {
		t.Fatalf("SMTPTLSMode = %q, want %q in development", cfg.SMTPTLSMode, "none")
	}

	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMTPTLSMode != "mandatory" {
		t.Fatalf("SMTPTLSMode = %q, want %q in production", cfg.SMTPTLSMode, "mandatory")
	}
}

func TestLoadRejectsAnUnknownSMTPTLSMode(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_TLS_MODE", "sometimes")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for an unknown SMTP_TLS_MODE")
	}
}

func TestLoadAcceptsAnExplicitSMTPTLSMode(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_TLS_MODE", "opportunistic")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMTPTLSMode != "opportunistic" {
		t.Fatalf("SMTPTLSMode = %q, want %q", cfg.SMTPTLSMode, "opportunistic")
	}
}

// TestLoadRejectsOneOfSMTPUsernameOrPasswordWithoutTheOther pins the pairing
// rule: a lone SMTP_USERNAME or SMTP_PASSWORD is never an intended
// deployment and would otherwise silently send unauthenticated.
func TestLoadRejectsOneOfSMTPUsernameOrPasswordWithoutTheOther(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_USERNAME", "relay-user")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when SMTP_USERNAME is set without SMTP_PASSWORD")
	}
}

func TestLoadAcceptsBothSMTPUsernameAndPassword(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_USERNAME", "relay-user")
	t.Setenv("SMTP_PASSWORD", "relay-pass")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMTPUsername != "relay-user" || cfg.SMTPPassword != "relay-pass" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsZeroArgon2Time(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ARGON2_TIME", "0")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when ARGON2_TIME is zero")
	}
}

// TestLoadRejectsArgon2ThreadsOverflow guards against a value that fits in
// an int but not in the uint8 field it is destined for: naively casting
// would wrap 256 to 0, silently turning a "positive" ARGON2_THREADS into
// zero threads once it reaches argon2.IDKey.
func TestLoadRejectsArgon2ThreadsOverflow(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ARGON2_THREADS", "256")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when ARGON2_THREADS overflows a uint8")
	}
}

func TestTelegramValuesMustBeBothOrNeither(t *testing.T) {
	setRequiredEnv(t) // existing helper: APP_ENV, DATABASE_URL, SMTP_*, APP_BASE_URL
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	// TELEGRAM_BOT_USERNAME deliberately unset.

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() with a token and no username succeeded, want an error")
	}
}

func TestTelegramDisabledWhenBothEmpty(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.TelegramEnabled() {
		t.Fatal("TelegramEnabled() = true with both values empty, want false")
	}
}

func TestTelegramEnabledWhenBothSet(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_BOT_USERNAME", "HearthBot")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if !cfg.TelegramEnabled() {
		t.Fatal("TelegramEnabled() = false with both values set, want true")
	}
	if cfg.TelegramBotUsername != "HearthBot" {
		t.Fatalf("TelegramBotUsername = %q, want %q", cfg.TelegramBotUsername, "HearthBot")
	}
}

func TestLoadAcceptsAnAbsentMailpitAPIURL(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MailpitAPIURL != "" {
		t.Fatalf("MailpitAPIURL = %q, want empty", cfg.MailpitAPIURL)
	}
	if cfg.OutboxEnabled() {
		t.Fatal("OutboxEnabled() = true with no URL set")
	}
}

func TestLoadAcceptsAMailpitAPIURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAILPIT_API_URL", "http://mailpit:8025")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.OutboxEnabled() {
		t.Fatal("OutboxEnabled() = false with a URL set")
	}
}

// A typo in .env must stop the boot, not present on the box as a 502 with
// nothing pointing at the line that caused it -- docs/LEARNING.md pattern 8,
// "configuration that lies".
func TestLoadRefusesAnUnusableMailpitAPIURL(t *testing.T) {
	for _, value := range []string{"mailpit:8025", "://mailpit", "ftp://mailpit:8025", "http:/mailpit:8025"} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("MAILPIT_API_URL", value)

			if _, err := config.Load(); err == nil {
				t.Fatalf("Load accepted MAILPIT_API_URL=%q", value)
			}
		})
	}
}

func TestLoadAcceptsAnAbsentDatabaseReadonlyURL(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseReadonlyURL != "" {
		t.Fatalf("DatabaseReadonlyURL = %q, want empty", cfg.DatabaseReadonlyURL)
	}
	if cfg.BrowseEnabled() {
		t.Fatal("BrowseEnabled() = true with no URL set")
	}
}

func TestLoadAcceptsADatabaseReadonlyURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_READONLY_URL", "postgres://hearth_readonly:pw@postgres:5432/hearth?sslmode=disable")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.BrowseEnabled() {
		t.Fatal("BrowseEnabled() = false with a URL set")
	}
}

// Unlike MAILPIT_API_URL, Load does NOT reject an unusable value here, and
// that is deliberate rather than an omission: net/url cannot tell a broken
// DSN from a legal keyword/value one, and pgxpool.ParseConfig -- which can --
// belongs to the adapter layer. postgres.OpenReadOnly is where a bad value
// refuses the boot; TestOpenReadOnlyRefusesAnUnparseableURL is its test. This
// test exists to record that the omission was decided, so that nobody
// "fixes" it later with a url.Parse that rejects a legal DSN.
func TestLoadDoesNotItselfValidateTheReadonlyDSN(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_READONLY_URL", "host=postgres user=hearth_readonly dbname=hearth")

	if _, err := config.Load(); err != nil {
		t.Fatalf("Load rejected a legal keyword/value DSN: %v", err)
	}
}
