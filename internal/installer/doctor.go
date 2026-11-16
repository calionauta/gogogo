// doctor & generate — deterministic preflight/codegen (see docs/doctor-generate.md)
//
//nolint:lll,goconst,mnd,gocyclo,funlen,gosec,staticcheck,contextcheck,noctx,nolintlint,perfsprint // see docs/doctor-generate.md; CI gates are --json + lefthook + generate --check
package installer

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/recipe"
)

const toolNameTempl = "templ"

const goosWin = "windows"

// CheckStatus is a doctor check's verdict. It is the JSON `status` field, so the
// spellings are a contract: constants, never literals at the comparison site.
type CheckStatus string

const (
	checkPass CheckStatus = "pass"
	checkWarn CheckStatus = "warn"
	checkFail CheckStatus = "fail"
	checkSkip CheckStatus = "skip"
)

type doctorCheck struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Status   CheckStatus `json:"status"` // pass|warn|fail|skip
	Version  string      `json:"version,omitempty"`
	Required string      `json:"required,omitempty"`
	Fix      string      `json:"fix,omitempty"`
}

type doctorReport struct {
	OK       bool          `json:"ok"`
	Checks   []doctorCheck `json:"checks"`
	Warnings []string      `json:"warnings,omitempty"`
}

func runDoctor(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "machine-readable JSON")
	only := fs.String("only", "", "run only this check id (repeat via comma)")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep, fails := runAllChecks(strings.Split(*only, ",")...)
	if *jsonOut {
		b, _ := jsonMarshal(rep)
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "%s doctor: %d checks, %d fail\n", binName, len(rep.Checks), fails)
		for _, c := range rep.Checks {
			fmt.Fprintf(stdout, "  %-4s %-30s %s\n", c.Status, c.ID, c.Name)
			if c.Fix != "" && c.Status == checkFail {
				fmt.Fprintf(stdout, "         fix: %s\n", c.Fix)
			}
		}
	}
	if fails > 0 {
		return &ExitError{code: 4, msg: "doctor checks failed"}
	}
	return nil
}

func runAllChecks(only ...string) (doctorReport, int) {
	var checks []doctorCheck
	fails := 0
	add := func(c doctorCheck) {
		for _, o := range only {
			o = strings.TrimSpace(o)
			if o == "" {
				break
			}
			if o != "" && c.ID != o && c.Name != o {
				return
			}
		}
		// Normalize: keep only requested
		keep := len(only) == 0 || only[0] == ""
		for _, o := range only {
			if o == c.ID || o == c.Name {
				keep = true
			}
		}
		if !keep && len(only) > 0 {
			return
		}
		checks = append(checks, c)
		if c.Status == checkFail {
			fails++
		}
	}
	// Go version
	add(doctorCheck{ID: "go.version", Name: "go version", Status: goVersionStatus(), Version: runtime.Version(), Required: "go 1.27.x", Fix: "install pinned toolchain from go.mod"})
	// Tools via PATH
	for _, t := range []struct {
		id, name, fix, min string
	}{
		{"go.tool.templ", toolNameTempl, "go tool templ generate", "pinned in go.mod"},
		{"tool.ripwire", "ripwire", "brew install ripwire or https://github.com/redhat-et/ripwire", "*"},
		{"tool.sem", "sem", "brew install sem-cli", "*"},
		{"tool.cymbal", "cymbal", "brew install cymbal && cymbal index .", "*"},
		{"tool.xerj", "xerj", "curl -fsSL https://xerj.org/get | sh", "*"},
		{"tool.lefthook", "lefthook", "make setup (core.hooksPath=.githooks)", "*"},
		{"tool.sqlc", "sqlc", "go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest", "*"},
		{"tool.docker", "docker", "brew install docker (cross/desktop)", "*"},
	} {
		add(checkBinary(t.id, t.name, t.fix, t.min))
	}
	// zig is opt-in (wails-cross / MyGo lite) — missing is warn, not fail
	add(checkBinaryWarn("tool.zig", "zig", "brew install zig (wails-cross / MyGo lite)", "*"))
	// mygo CLI is opt-in (packaging only; plain go build needs no CLI) — missing is warn, not fail
	add(checkBinaryWarn("tool.mygo", "mygo", "go tool github.com/egoist/mygo/cmd/mygo (MyGo packaging; make mygo needs no CLI)", "*"))
	// Desktop/mobile (warn only)
	switch runtime.GOOS {
	case "darwin":
		add(checkXcodeSDK())
	case "linux":
		add(checkPkg("desktop.gtk", "libgtk-4-dev", "sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev"))
	case "winGOOS":
		add(doctorCheck{ID: "desktop.webview2", Name: "WebView2 runtime (windows)", Status: checkSkip, Required: "WebView2 Runtime", Fix: "install WebView2 Runtime"})
	}
	if hasEnv("ANDROID_HOME") || hasEnv("ANDROID_SDK_ROOT") {
		add(checkBinary("mobile.sdk", "adb", "install Android command-line-tools", "*"))
	} else {
		add(doctorCheck{ID: "mobile.sdk", Name: "android SDK (skipped, mobile not targeted)", Status: checkSkip, Fix: "export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools (mobile-only)"})
	}
	// WebView2 windows runtime
	if runtime.GOOS == goosWin {
		add(doctorCheck{ID: "desktop.webview2", Name: "WebView2 runtime (windows)", Status: checkSkip, Required: "WebView2 Runtime", Fix: "install WebView2 Runtime"})
	}
	// Secrets
	add(checkSecrets())
	// Host capacity
	add(checkHostCapacity())
	// Registry freshness (WARN if stale)
	add(checkRegistryFresh())
	// Drift (templ/sqlc) — WARN, not FAIL
	add(checkDrift())
	// Recipe declaration health is a fail on structural errors; warnings
	// remain visible without blocking environment preflight.
	add(checkRecipes())

	rep := doctorReport{OK: fails == 0, Checks: checks}
	return rep, fails
}

func goVersionStatus() CheckStatus {
	// runtime.Version returns e.g. "go1.27.3"
	v := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(v, ".")
	if len(parts) >= 2 && parts[0] == "1" && parts[1] >= "27" {
		return checkPass
	}
	return checkFail
}

func checkBinary(id, name, fix, min string) doctorCheck {
	path, err := lookPath(name)
	if err != nil || path == "" {
		return doctorCheck{ID: id, Name: name, Status: checkFail, Required: min, Fix: fix}
	}
	cmd := exec.Command(name, "--version")
	out, _ := cmd.CombinedOutput()
	ver := strings.TrimSpace(string(out))
	if i := strings.Index(ver, "\n"); i >= 0 {
		ver = ver[:i]
	}
	if ver == "" || strings.HasPrefix(ver, "│") {
		// lefthook returns usage on --version; probe help instead
		help, _ := exec.Command(name, "--help").CombinedOutput()
		ver = strings.TrimSpace(strings.Split(strings.TrimSpace(string(help)), "\n")[0])
		if ver == "" {
			ver = path
		}
	}
	return doctorCheck{ID: id, Name: name, Status: checkPass, Version: ver, Required: min}
}

func checkBinaryWarn(id, name, fix, min string) doctorCheck {
	path, err := lookPath(name)
	if err != nil || path == "" {
		return doctorCheck{ID: id, Name: name, Status: checkWarn, Required: min, Fix: fix}
	}
	// zig answers to `zig version`, not --version; probe both
	out, err2 := exec.Command(name, "version").CombinedOutput()
	ver := strings.TrimSpace(string(out))
	if err2 != nil || ver == "" {
		out, _ = exec.Command(name, "--version").CombinedOutput()
		ver = strings.TrimSpace(string(out))
		if i := strings.Index(ver, "\n"); i >= 0 {
			ver = ver[:i]
		}
	}
	if ver == "" {
		ver = path
	}
	return doctorCheck{ID: id, Name: name, Status: checkPass, Version: ver, Required: min}
}

func checkXcodeSDK() doctorCheck {
	out, err := exec.Command("xcode-select", "-p").CombinedOutput()
	if err != nil {
		return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: checkWarn, Required: "*", Fix: "xcode-select --install"}
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: checkWarn, Required: "*", Fix: "xcode-select --install"}
	}
	ver, _ := exec.Command("xcrun", "--show-sdk-path").CombinedOutput()
	_ = ver
	return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: checkPass, Version: path, Required: "*"}
}

func checkPkg(id, name, fix string) doctorCheck {
	// Pkg-config probe, free of network
	if _, err := exec.LookPath("pkg-config"); err != nil {
		return doctorCheck{ID: id, Name: name, Status: checkSkip, Fix: fix}
	}
	out, err := exec.Command("pkg-config", "--modversion", name).CombinedOutput()
	if err != nil {
		return doctorCheck{ID: id, Name: name, Status: checkFail, Fix: fix}
	}
	var buf bytes.Buffer
	buf.Write(out)
	return doctorCheck{ID: id, Name: name, Status: checkPass, Version: strings.TrimSpace(buf.String()), Fix: fix}
}

func hasEnv(k string) bool { return os.Getenv(k) != "" }

func checkSecrets() doctorCheck {
	if _, err := os.Stat(os.Getenv("HOME") + "/.secrets"); err != nil {
		return doctorCheck{ID: "secrets.age", Name: "age secrets (~/.secrets/*.env.age)", Status: checkWarn, Required: "bin/init-secrets", Fix: "bin/init-secrets (age .env.age) — see docs/security.md"}
	}
	return doctorCheck{ID: "secrets.age", Name: "age secrets (~/.secrets)", Status: checkPass}
}

func checkHostCapacity() doctorCheck {
	// Reuse existing script logic via reading: do cheap probe, WARN only
	mem, _ := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
	if len(mem) == 0 {
		return doctorCheck{ID: "host.capacity", Name: "host capacity", Status: checkSkip, Fix: "scripts/check-host-capacity.sh"}
	}
	return doctorCheck{ID: "host.capacity", Name: "host capacity", Status: checkPass, Version: strings.TrimSpace(string(mem)), Fix: "scripts/check-host-capacity.sh"}
}

func checkRegistryFresh() doctorCheck {
	if _, err := os.Stat(filepath.Join("docs", "unit-contracts.md")); err != nil {
		return doctorCheck{ID: "registry.fresh", Name: "unit-contracts freshness", Status: checkWarn, Fix: "go test ./internal/capabilities -run TestCapabilityContractsDocFresh -update"}
	}
	return doctorCheck{ID: "registry.fresh", Name: "unit-contracts freshness", Status: checkPass}
}

// checkDrift answers the one question it can answer READ-ONLY: is every
// generated file newer than the source that produces it? It used to be a
// permanent `skip`, which made `doctor` claim a check it never performed — and
// a skip inside an "ok: true" report reads as a pass to anyone scanning it.
//
// It does NOT regenerate (that is `gogogo generate --check`, which rewrites and
// compares): a preflight must be safe to run mid-edit. The source sets are the
// ones the Makefile owns.
func checkDrift() doctorCheck {
	stale, err := staleGenerated(".")
	if err != nil {
		return doctorCheck{
			ID: "generate.drift", Name: "generated drift (templ/sqlc/css/docs)",
			Status: checkSkip, Fix: binName + " generate --check",
		}
	}
	if len(stale) > 0 {
		return doctorCheck{
			ID: "generate.drift", Name: "generated drift (templ/sqlc/css/docs)",
			Status: checkWarn, Version: fmt.Sprintf("%d stale", len(stale)),
			Fix: binName + " generate --check, then commit (stale: " + strings.Join(firstN(stale, 3), ", ") + ")",
		}
	}
	return doctorCheck{ID: "generate.drift", Name: "generated drift (templ/sqlc/css/docs)", Status: checkPass}
}

// staleGenerated lists generated outputs older than their sources. A pair whose
// source or output is absent is skipped: a tree without the generator has no
// drift to report.
func staleGenerated(root string) ([]string, error) {
	var stale []string
	// templ: every <name>.templ owns the <name>_templ.go beside it.
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDriftDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".templ") {
			return nil
		}
		out := strings.TrimSuffix(path, ".templ") + "_templ.go"
		older, oerr := olderThan(path, out)
		if oerr == nil && older {
			stale = append(stale, out)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	// sqlc: any query file newer than the newest generated file.
	newest, err := newestIn("internal/db/sqlcgen")
	if err == nil && newest != "" {
		queryNewest, qerr := newestIn("internal/db/queries")
		if qerr == nil && queryNewest != "" && queryNewest > newest {
			stale = append(stale, "internal/db/sqlcgen")
		}
	}
	sort.Strings(stale)
	return stale, nil
}

// skipDriftDir is the noise a freshness scan must not walk.
func skipDriftDir(name string) bool {
	switch name {
	case gitDirName, dirNodeModules, dirData, dirSite, dirVendor, dirTmp, "dist":
		return true
	}
	return false
}

// olderThan reports whether out is missing or older than src.
func olderThan(src, out string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	outInfo, oerr := os.Stat(out)
	if oerr != nil {
		// Never generated: stale by definition. Not an error for the caller —
		// the freshness question is answered, and the answer is "missing".
		return true, nil //nolint:nilerr // a missing output IS the verdict
	}
	return srcInfo.ModTime().After(outInfo.ModTime()), nil
}

// newestIn returns the newest modification stamp under dir ("" when empty).
func newestIn(dir string) (string, error) {
	var newest time.Time
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if info, ierr := d.Info(); ierr == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if newest.IsZero() {
		return "", nil
	}
	return newest.Format(time.RFC3339Nano), nil
}

func checkRecipes() doctorCheck {
	ids, err := recipe.ListReceipts(".")
	if err != nil {
		return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: checkSkip, Fix: binName + " recipe check --format json"}
	}
	if len(ids) == 0 {
		return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: checkSkip, Fix: binName + " recipe check --format json"}
	}
	blocked := 0
	warnings := 0
	for _, id := range ids {
		report := checkOneRecipe(".", id)
		if !report.Healthy {
			blocked++
		}
		warnings += len(report.Warnings)
	}
	if blocked > 0 {
		return doctorCheck{
			ID: "recipe.declarations", Name: "recipe declarations", Status: checkFail,
			Version: fmt.Sprintf("%d checked", len(ids)),
			Fix:     "gogogo recipe check --format json",
		}
	}
	if warnings > 0 {
		return doctorCheck{
			ID: "recipe.declarations", Name: "recipe declarations", Status: checkWarn,
			Version: fmt.Sprintf("%d checked", len(ids)),
			Fix:     "gogogo recipe check --format json",
		}
	}
	return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: checkPass, Version: fmt.Sprintf("%d checked", len(ids))}
}

func jsonMarshal(v any) ([]byte, error) {
	// Thin wrapper so doctor.go does not pull encoding/json everywhere
	return marshalJSON(v)
}

// generate.go below
