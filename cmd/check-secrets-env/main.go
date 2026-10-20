// SCOPE:core — Secrets-env site guard (secret env reads stay on the rails)
//
// Command check-secrets-env proves secret environment plumbing never
// bypasses the age-secrets standard: os.Getenv/LookupEnv/Setenv with a
// secret-shaped name (*_KEY, *_TOKEN, *_SECRET, *_PASSWORD, …) may only
// run in owner files — the config loader, the secrets loader, and the
// two narrowly allowlisted call sites below. Values are gitleaks' job
// (pre-commit + CI); this tool guards SITES, so a new key either lands
// in config (documented, masked, age-sourced) or fails the gate with
// the file and line named.
//
// An LLM adding "just one quick env read with a key-shaped name" in a
// handler would silently route a credential around the age file, the /config
// masking, and the rotation story — so the allowlist is explicit with a
// reason per entry, not a policy paragraph nobody reads. Textual scan
// (same precedent as the sql.Open owner check in check-flatdeps):
// test files are exempt (t.Setenv fixtures; values still gated by
// gitleaks), everything else is not.
//
// Exit non-zero naming every offending file:line when the invariant breaks.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// envCall matches os.Getenv("NAME"), os.LookupEnv("NAME", …) and
// os.Setenv("NAME", …) with a double-quoted literal name. Concatenated
// or computed names do not match — build the name from one literal so
// the scanner (and the next reader) can see it.
var envCall = regexp.MustCompile(`os\.(Getenv|LookupEnv|Setenv)\(\s*"([A-Z0-9_]+)"`)

// secretSegments are the name segments that mark a credential. Matched
// per underscore-separated segment (not substring), so MONKEY or
// TURKEY never trip it while API_KEY, AUTH_TOKEN and ENC_KEY always do.
var secretSegments = map[string]bool{
	"KEY": true, "TOKEN": true, "TOKENS": true,
	"SECRET": true, "SECRETS": true,
	"PASSWORD": true, "PASSWORDS": true, "PASSWD": true,
	"CREDENTIALS": true, "CREDENTIAL": true,
}

// ownerFiles are the only non-test Go files allowed to touch secret env.
// Each entry carries its reason inline: a new entry is the review moment
// the guarantee needs (same rule as flatPackages in check-flatdeps).
var ownerFiles = []string{
	// Single source of truth: every secret env lands on config.Config
	// here (documented in the file header, masked on /config).
	"config/config.go",
	"config/auth_env.go",
	"config/review.go",
	// The age loader: decrypts ~/.secrets/*.env.age into the process
	// env and writes nothing back to disk.
	"internal/secrets/secrets.go",
	// Malformed-key fail-loud checks: config drops a malformed key
	// silently, so both server entries refuse an explicit-but-broken
	// operator value at boot instead of running half-encrypted.
	"internal/server/boot_helpers.go",
	"cmd/gui/main.go",
	// /config display: AGE_SECRET_KEY boolean-only read (the value
	// never leaves the process; safe_view_test pins it).
	"features/config/components/safe_view.go",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "check-secrets-env:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	offenders, err := collectOffenders(root)
	if err != nil {
		return err
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf("secret env read outside owner files (%d):\n  %s\n%s",
			len(offenders), strings.Join(offenders, "\n  "), ownerHint)
	}
	fmt.Fprintln(os.Stdout, "check-secrets-env: secret env stays on the rails")
	return nil
}

// ownerHint is the remediation printed with every violation (kept beside
// the format string so message and guidance never drift apart).
const ownerHint = "route new keys through config (documented, masked, " +
	"age-sourced) or extend ownerFiles with a reason"

// scanBufferBytes caps one source line for the line scanner (64 KiB is
// generous for Go; longer lines fail loudly instead of truncating).
const scanBufferBytes = 64 * 1024

// collectOffenders walks the module tree and returns rel:line:NAME for
// every secret-shaped env call outside owner files. Split from run for
// the gocyclo gate.
func collectOffenders(root string) ([]string, error) {
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == ".git" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isOwner(rel) {
			return nil
		}
		hits, err := secretReadsOutsideOwners(path)
		if err != nil {
			return err
		}
		for _, h := range hits {
			offenders = append(offenders, rel+":"+h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return offenders, nil
}

// secretReadsOutsideOwners returns "line:NAME" for every secret-shaped
// env call in the file. Ownership is decided by the caller.
func secretReadsOutsideOwners(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hits []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, scanBufferBytes), scanBufferBytes)
	line := 0
	for sc.Scan() {
		line++
		m := envCall.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if isSecretName(m[2]) {
			hits = append(hits, fmt.Sprintf("%d:%s", line, m[2]))
		}
	}
	return hits, sc.Err()
}

// isSecretName reports whether any underscore-separated segment marks a
// credential. Exact-segment match (not substring): MONKEY stays clean.
func isSecretName(name string) bool {
	for part := range strings.SplitSeq(name, "_") {
		if secretSegments[part] {
			return true
		}
	}
	return false
}

func isOwner(rel string) bool {
	return slices.Contains(ownerFiles, rel)
}

// moduleRoot walks up from the working directory to the module root
// (the dir holding go.mod). `go run` starts at the developer's shell
// dir, but `go test` starts inside the package dir.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above working directory")
		}
		dir = parent
	}
}
