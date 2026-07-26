package config_test

import (
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/config"
)

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://hearth:hearth@localhost:5432/hearth?sslmode=disable")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "development" || cfg.Port != 8080 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is empty")
	}
}

func TestLoadRejectsUnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for an unknown APP_ENV")
	}
}
