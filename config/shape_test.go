package config

import "testing"

func TestAppShapeFromEnv(t *testing.T) {
	t.Setenv("APP_SHAPE", "api")
	if cfg := Load(); !cfg.IsAPIShape() {
		t.Fatalf("APP_SHAPE=api -> %q, want api", cfg.AppShape)
	}
	t.Setenv("APP_SHAPE", "nonsense")
	if cfg := Load(); cfg.IsAPIShape() {
		t.Fatalf("an unknown shape must fall back to full, got %q", cfg.AppShape)
	}
}
