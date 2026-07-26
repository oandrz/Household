// Package config turns environment variables into a validated Config value.
// It is the only place in the service that reads os.Getenv.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	AppEnv        string
	Port          int
	DatabaseURL   string
	SessionSecret string
}

func (c Config) IsDevelopment() bool { return c.AppEnv == "development" }

func Load() (Config, error) {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		return Config{}, fmt.Errorf("APP_ENV is required (development, test or production)")
	}

	cfg := Config{
		AppEnv:        appEnv,
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
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
	if len(cfg.SessionSecret) < 32 {
		return Config{}, fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
