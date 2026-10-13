package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Red-proof discipline: each bad tree below must fail naming the file,
// each good tree must pass. Invert any assertion (swap wantErr) and the
// suite goes red — nothing here is green-by-construction.

func TestIsSecretName(t *testing.T) {
	secret := []string{
		"GOAI_API_KEY", "AUTH_ENC_KEY", "ADMIN_UNLOCK_TOKEN",
		"CREDITS_ENC_KEY", "STRIPE_SECRET_KEY", "JEV_API_KEY",
		"TURSO_AUTH_TOKEN", "AGE_SECRET_KEY", "DB_PASSWORD",
	}
	for _, name := range secret {
		if !isSecretName(name) {
			t.Errorf("isSecretName(%q) = false, want true", name)
		}
	}
	clean := []string{
		"GOAI_BASE_URL", "GOAI_MODEL", "JEV_TIMEOUT", "JEV_MODEL",
		"SIMULATE_LLM", "PORT", "DATA_DIR", "MONKEY", "TURKEY",
		"BYOK_PROVIDERS", "ENVIRONMENT",
	}
	for _, name := range clean {
		if isSecretName(name) {
			t.Errorf("isSecretName(%q) = true, want false", name)
		}
	}
}

func TestOwnerFilesExist(t *testing.T) {
	// An allowlist entry pointing at a moved file is a lie that passes:
	// every owner must resolve inside the module.
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ownerFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(o))); err != nil {
			t.Errorf("owner file %q missing: %v", o, err)
		}
	}
}

// writeTree builds a scratch module-shaped tree under t.TempDir and
// returns a chdir'd runner: run() anchors at go.mod via moduleRoot, so
// tests must execute with the temp dir as working directory.
func writeTree(t *testing.T, files map[string]string) (restore func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanTreePasses(t *testing.T) {
	restore := writeTree(t, map[string]string{
		"config/config.go":     "package config\nvar _ = os.Getenv(\"GOAI_API_KEY\")\n",
		"features/x/chi.go":    "package chi\nvar _ = os.Getenv(\"GOAI_BASE_URL\")\n",
		"features/x/a_test.go": "package chi\nvar _ = os.Getenv(\"ANY_KEY\")\n",
	})
	defer restore()
	// ownerFiles points at the real tree, not the scratch: rewrite it
	// for this test so only the scratch config file is allowed.
	saved := ownerFiles
	ownerFiles = []string{"config/config.go"}
	defer func() { ownerFiles = saved }()
	if err := run(); err != nil {
		t.Fatalf("clean tree must pass, got: %v", err)
	}
}

func TestSecretReadOutsideOwnersFails(t *testing.T) {
	restore := writeTree(t, map[string]string{
		"features/sneaky/chi.go": "package chi\nvar _ = os.Getenv(\"SNEAKY_API_KEY\")\n",
	})
	defer restore()
	saved := ownerFiles
	ownerFiles = []string{"config/config.go"}
	defer func() { ownerFiles = saved }()
	err := run()
	if err == nil {
		t.Fatal("sneaky secret read must fail, passed")
	}
}

func TestSetenvOutsideOwnersFails(t *testing.T) {
	restore := writeTree(t, map[string]string{
		"internal/x/x.go": "package x\nfunc f() { _ = os.Setenv(\"X_TOKEN\", \"v\") }\n",
	})
	defer restore()
	saved := ownerFiles
	ownerFiles = []string{"internal/secrets/secrets.go"}
	defer func() { ownerFiles = saved }()
	if err := run(); err == nil {
		t.Fatal("off-owner Setenv must fail, passed")
	}
}
