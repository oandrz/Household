// Package config turns environment variables into a validated Config value.
// It is the only place in the service that reads os.Getenv.
package config

import (
	"fmt"
	"os"
	"strconv"
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
}

func (c Config) IsDevelopment() bool { return c.AppEnv == "development" }

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

	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
