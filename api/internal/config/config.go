// Package config turns environment variables into a validated Config value.
// It is the only place in the service that reads os.Getenv.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv      string
	Port        int
	DatabaseURL string
	SMTPAddr    string
	SMTPFrom    string
	// SMTPUsername and SMTPPassword are optional: empty (the default) means
	// no SMTP AUTH is attempted at all, which is what talking to Mailpit
	// (development) needs. Set both together for a relay that requires
	// authentication -- see SMTPTLSMode's doc comment for why "both or
	// neither" is the only combination Load accepts.
	SMTPUsername string
	SMTPPassword string
	// SMTPTLSMode is one of "none", "opportunistic" or "mandatory",
	// controlling whether the mailer requires, attempts, or never uses
	// STARTTLS. It defaults to "none" in development (Mailpit speaks plain
	// SMTP on the Compose network, and requiring TLS there would break it)
	// and to "mandatory" everywhere else: no hosted relay accepts an
	// unauthenticated, unencrypted connection, and the send is fire-and-
	// forget (see usecase/auth.go's sendMagicLinkAsync), so a silently
	// downgraded or rejected connection would otherwise never surface
	// anywhere a caller or an operator could see it.
	SMTPTLSMode     string
	AppBaseURL      string
	Argon2Time      uint32
	Argon2MemoryKiB uint32
	Argon2Threads   uint8
	// TelegramBotToken and TelegramBotUsername are optional and travel
	// together: both set turns Telegram sign-in on, both empty leaves it off.
	// One without the other is refused for the same reason SMTP_USERNAME and
	// SMTP_PASSWORD are -- a half-configured channel misbehaves silently, and
	// the symptom (links that are minted and never delivered) looks exactly
	// like nobody using the feature.
	//
	// The username is configured rather than read from Telegram's getMe at
	// startup: no cleverness, and no startup dependency on Telegram being
	// reachable.
	TelegramBotToken    string
	TelegramBotUsername string
	// MailpitAPIURL is Mailpit's HTTP API, http://mailpit:8025 in both
	// Compose stacks. Optional: empty means the operator's outbound message
	// inspector is unavailable and says so, rather than showing an empty
	// list -- an empty list would read as "Hearth has sent no mail".
	//
	// A value that is set but unusable refuses the boot, for the same reason
	// the SMTP and Telegram pairs do: a typo here would otherwise present on
	// the box as a 502 on one admin screen, with nothing pointing back at the
	// .env line that caused it.
	MailpitAPIURL string
	// DatabaseReadonlyURL is the DSN for hearth_readonly, the SELECT-only
	// role the operator's database browse reads through
	// (deploy/readonly-role.sql creates it). Optional: empty means the
	// browse is unavailable and says which variable is missing.
	//
	// There is deliberately no fallback to DatabaseURL. A half-provisioned
	// box degrades to "you cannot use this panel", never to "you are using
	// it through the read-write connection".
	//
	// It is NOT validated here, unlike every other optional value in this
	// file. net/url cannot tell a broken DSN from a legal keyword/value one
	// ("host=db user=x" parses fine and is valid), and the only honest
	// parser is pgxpool.ParseConfig, which belongs to the adapter layer --
	// this package imports the standard library and nothing else, and that
	// is worth more than moving one error message. postgres.OpenReadOnly
	// refuses the boot on a value it cannot parse, and on one that connects
	// as a role which can write.
	DatabaseReadonlyURL string
	// OpenRouterAPIKey and OpenRouterModel turn free-text intent parsing on
	// for the Telegram bot through OpenRouter (adapter/openrouter). They
	// travel together: a key with no model has nothing to call, and a model
	// with no key cannot call it. The model is never defaulted in code,
	// because the free, tool-capable models OpenRouter offers change month
	// to month; up to three, comma-separated, are tried in order. Empty is
	// the default and means commands only.
	OpenRouterAPIKey string
	OpenRouterModel  string
	// NudgesAt and NudgesLocation turn the daily Telegram digest on: a local
	// "HH:MM" and the IANA zone it is read in. Both or neither, and only
	// with Telegram configured, since a digest with no channel is a
	// misconfiguration rather than "off". One zone for the whole install
	// is a known gap (the tracker names it): right for one household,
	// wrong the day a second one signs up from another zone.
	NudgesAt       string
	NudgesLocation *time.Location
	// TrustedProxies are the networks allowed to tell the API who the client
	// is, through the X-Real-IP header (TRUSTED_PROXY_CIDRS, comma-separated
	// CIDRs). A request from anywhere else is keyed by the address that
	// actually connected and its headers are ignored. Empty -- the default --
	// trusts nobody; Load says why that is the safe way round.
	TrustedProxies []netip.Prefix
}

func (c Config) IsDevelopment() bool { return c.AppEnv == "development" }

// TelegramEnabled reports whether Telegram sign-in is configured. When it is
// false the route answers 404 and the poller never starts, so an install that
// has not set up a bot behaves exactly as it did before this feature existed.
func (c Config) TelegramEnabled() bool { return c.TelegramBotToken != "" }

// IntentParsingEnabled reports whether the Telegram bot reads free text.
// Off means the bot answers a sentence with /help; the slash commands work
// either way. Load has already enforced that the key and the model come
// together, so one of them is enough to ask.
func (c Config) IntentParsingEnabled() bool { return c.OpenRouterAPIKey != "" }

// NudgesEnabled reports whether the daily digest runs. Load has already
// enforced that the clock and the zone come together and that Telegram is on.
func (c Config) NudgesEnabled() bool { return c.NudgesAt != "" }

// OutboxEnabled reports whether the outbound message inspector is configured.
// When it is false the admin routes answer 503 and say which variable is
// missing -- never 404, because everyone who can reach them has already
// proved they are a platform admin with a live grant, and hiding the route
// from them would cost them the one fact that tells them what to fix.
func (c Config) OutboxEnabled() bool { return c.MailpitAPIURL != "" }

// BrowseEnabled reports whether the operator's database browse is configured.
// When it is false the admin routes answer 503 and name the variable -- never
// 404, because everyone who can reach them has already proved they are a
// platform admin with a live grant, and hiding the route from them would cost
// them the one fact that says what to fix.
func (c Config) BrowseEnabled() bool { return c.DatabaseReadonlyURL != "" }

func Load() (Config, error) {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		return Config{}, fmt.Errorf("APP_ENV is required (development, test or production)")
	}

	cfg := Config{
		AppEnv:       appEnv,
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		SMTPAddr:     os.Getenv("SMTP_ADDR"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		AppBaseURL:   os.Getenv("APP_BASE_URL"),

		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramBotUsername: os.Getenv("TELEGRAM_BOT_USERNAME"),
		MailpitAPIURL:       os.Getenv("MAILPIT_API_URL"),
		DatabaseReadonlyURL: os.Getenv("DATABASE_READONLY_URL"),
		OpenRouterAPIKey:    os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:     os.Getenv("OPENROUTER_MODEL"),
	}

	switch cfg.AppEnv {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV must be development, test or production, got %q", cfg.AppEnv)
	}

	port, err := strconv.Atoi(env("PORT", "8080"))
	if err != nil {
		return Config{}, fmt.Errorf("PORT must be a number: %w", err)
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be between 1 and 65535, got %d", port)
	}
	cfg.Port = port

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SMTPAddr == "" {
		return Config{}, fmt.Errorf("SMTP_ADDR is required")
	}
	if cfg.SMTPFrom == "" {
		return Config{}, fmt.Errorf("SMTP_FROM is required")
	}
	// Both or neither: a username with no password (or vice versa) is never
	// a deployment anyone intends, and silently sending unauthenticated in
	// that case would mask a misconfigured relay behind mail that appears to
	// send fine right up until the relay actually starts rejecting it.
	if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
		return Config{}, fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must both be set, or both left empty")
	}
	if (cfg.TelegramBotToken == "") != (cfg.TelegramBotUsername == "") {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_USERNAME must both be set, or both left empty")
	}
	if (cfg.OpenRouterAPIKey == "") != (cfg.OpenRouterModel == "") {
		return Config{}, fmt.Errorf("OPENROUTER_API_KEY and OPENROUTER_MODEL must both be set, or both left empty")
	}
	nudgesAt, nudgesTZ := os.Getenv("NUDGES_AT"), os.Getenv("NUDGES_TIMEZONE")
	if (nudgesAt == "") != (nudgesTZ == "") {
		return Config{}, fmt.Errorf("NUDGES_AT and NUDGES_TIMEZONE must both be set, or both left empty")
	}
	if nudgesAt != "" {
		if !cfg.TelegramEnabled() {
			return Config{}, fmt.Errorf("NUDGES_AT is set but Telegram is not configured; the digest has no channel")
		}
		if _, err := time.Parse("15:04", nudgesAt); err != nil {
			return Config{}, fmt.Errorf(`NUDGES_AT must be a 24-hour clock like "09:00", got %q`, nudgesAt)
		}
		loc, err := time.LoadLocation(nudgesTZ)
		if err != nil {
			return Config{}, fmt.Errorf("NUDGES_TIMEZONE must be an IANA zone like Asia/Singapore, got %q", nudgesTZ)
		}
		cfg.NudgesAt, cfg.NudgesLocation = nudgesAt, loc
	}
	if cfg.MailpitAPIURL != "" {
		parsed, err := url.Parse(cfg.MailpitAPIURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, fmt.Errorf(`MAILPIT_API_URL must be an http or https URL, got %q`, cfg.MailpitAPIURL)
		}
	}
	defaultTLSMode := "mandatory"
	if cfg.IsDevelopment() {
		defaultTLSMode = "none"
	}
	cfg.SMTPTLSMode = env("SMTP_TLS_MODE", defaultTLSMode)
	switch cfg.SMTPTLSMode {
	case "none", "opportunistic", "mandatory":
	default:
		return Config{}, fmt.Errorf(`SMTP_TLS_MODE must be "none", "opportunistic" or "mandatory", got %q`, cfg.SMTPTLSMode)
	}
	if cfg.AppBaseURL == "" {
		return Config{}, fmt.Errorf("APP_BASE_URL is required")
	}

	// ParseUint with an explicit bit size rejects a value that would overflow
	// the field it is destined for (e.g. ARGON2_THREADS=256) instead of
	// silently wrapping to zero on the uint8/uint32 cast, which would hand
	// argon2.IDKey a "positive" configuration that is actually zero threads.
	argon2Time, err := strconv.ParseUint(env("ARGON2_TIME", "3"), 10, 32)
	if err != nil {
		return Config{}, fmt.Errorf("ARGON2_TIME must be a positive number that fits in 32 bits: %w", err)
	}
	if argon2Time == 0 {
		return Config{}, fmt.Errorf("ARGON2_TIME must be positive, got %d", argon2Time)
	}
	cfg.Argon2Time = uint32(argon2Time)

	argon2MemoryKiB, err := strconv.ParseUint(env("ARGON2_MEMORY_KIB", "65536"), 10, 32)
	if err != nil {
		return Config{}, fmt.Errorf("ARGON2_MEMORY_KIB must be a positive number that fits in 32 bits: %w", err)
	}
	if argon2MemoryKiB == 0 {
		return Config{}, fmt.Errorf("ARGON2_MEMORY_KIB must be positive, got %d", argon2MemoryKiB)
	}
	cfg.Argon2MemoryKiB = uint32(argon2MemoryKiB)

	argon2Threads, err := strconv.ParseUint(env("ARGON2_THREADS", "2"), 10, 8)
	if err != nil {
		return Config{}, fmt.Errorf("ARGON2_THREADS must be a positive number that fits in 8 bits: %w", err)
	}
	if argon2Threads == 0 {
		return Config{}, fmt.Errorf("ARGON2_THREADS must be positive, got %d", argon2Threads)
	}
	cfg.Argon2Threads = uint8(argon2Threads)

	// TRUSTED_PROXY_CIDRS fails closed: unset trusts no proxy at all, so every
	// request is keyed by the address that actually connected. The opposite
	// default -- believe client-address headers from anyone -- is what chi's
	// RealIP did, and it let any caller who reached the API directly choose
	// the IP the sign-up limiter and the admin audit log see. Forgetting this
	// variable now costs a limiter that counts every visitor as nginx: a loud
	// 429 that someone reports. Forgetting it under the old default cost the
	// limit itself, silently. A malformed entry refuses the boot rather than
	// being skipped, because a skipped entry is that same shared bucket with
	// nothing pointing back at the .env line.
	trusted, err := parseTrustedProxies(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxies = trusted

	return cfg, nil
}

// parseTrustedProxies reads TRUSTED_PROXY_CIDRS: comma-separated CIDRs such
// as "172.28.0.0/16", or "10.0.0.5/32" for a single host. A bare address is
// refused rather than guessed at as a /32, so the operator writes the width
// they mean.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var prefixes []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS must be comma-separated CIDRs like 172.28.0.0/16, got %q", entry)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
