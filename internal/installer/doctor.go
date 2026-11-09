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
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

const toolNameTempl = "templ"

const goosWin = "windows"

const (
	checkPass = "pass"
	checkWarn = "warn"
	checkFail = "fail"
	checkSkip = "skip"
)

type doctorCheck struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"` // pass|warn|fail|skip
	Version  string `json:"version,omitempty"`
	Required string `json:"required,omitempty"`
	Fix      string `json:"fix,omitempty"`
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
		fmt.Fprintf(stdout, "gogogo doctor: %d checks, %d fail\n", len(rep.Checks), fails)
		for _, c := range rep.Checks {
			fmt.Fprintf(stdout, "  %-4s %-30s %s\n", c.Status, c.ID, c.Name)
			if c.Fix != "" && c.Status == "fail" {
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
		if c.Status == "fail" {
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
		add(doctorCheck{ID: "mobile.sdk", Name: "android SDK (skipped, mobile not targeted)", Status: "skip", Fix: "export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools (mobile-only)"})
	}
	// WebView2 windows runtime
	if runtime.GOOS == goosWin {
		add(doctorCheck{ID: "desktop.webview2", Name: "WebView2 runtime (windows)", Status: "skip", Required: "WebView2 Runtime", Fix: "install WebView2 Runtime"})
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

func goVersionStatus() string {
	// runtime.Version returns e.g. "go1.27.3"
	v := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(v, ".")
	if len(parts) >= 2 && parts[0] == "1" && parts[1] >= "27" {
		return "pass"
	}
	return "fail"
}

func checkBinary(id, name, fix, min string) doctorCheck {
	path, err := lookPath(name)
	if err != nil || path == "" {
		return doctorCheck{ID: id, Name: name, Status: "fail", Required: min, Fix: fix}
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
	return doctorCheck{ID: id, Name: name, Status: "pass", Version: ver, Required: min}
}

func checkBinaryWarn(id, name, fix, min string) doctorCheck {
	path, err := lookPath(name)
	if err != nil || path == "" {
		return doctorCheck{ID: id, Name: name, Status: "warn", Required: min, Fix: fix}
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
	return doctorCheck{ID: id, Name: name, Status: "pass", Version: ver, Required: min}
}

func checkXcodeSDK() doctorCheck {
	out, err := exec.Command("xcode-select", "-p").CombinedOutput()
	if err != nil {
		return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: "warn", Required: "*", Fix: "xcode-select --install"}
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: "warn", Required: "*", Fix: "xcode-select --install"}
	}
	ver, _ := exec.Command("xcrun", "--show-sdk-path").CombinedOutput()
	_ = ver
	return doctorCheck{ID: "desktop.sdk", Name: "xcode", Status: "pass", Version: path, Required: "*"}
}

func checkPkg(id, name, fix string) doctorCheck {
	// Pkg-config probe, free of network
	if _, err := exec.LookPath("pkg-config"); err != nil {
		return doctorCheck{ID: id, Name: name, Status: "skip", Fix: fix}
	}
	out, err := exec.Command("pkg-config", "--modversion", name).CombinedOutput()
	if err != nil {
		return doctorCheck{ID: id, Name: name, Status: "fail", Fix: fix}
	}
	var buf bytes.Buffer
	buf.Write(out)
	return doctorCheck{ID: id, Name: name, Status: "pass", Version: strings.TrimSpace(buf.String()), Fix: fix}
}

func hasEnv(k string) bool { return os.Getenv(k) != "" }

func checkSecrets() doctorCheck {
	if _, err := os.Stat(os.Getenv("HOME") + "/.secrets"); err != nil {
		return doctorCheck{ID: "secrets.age", Name: "age secrets (~/.secrets/*.env.age)", Status: "warn", Required: "bin/init-secrets", Fix: "bin/init-secrets (age .env.age) — see docs/security.md"}
	}
	return doctorCheck{ID: "secrets.age", Name: "age secrets (~/.secrets)", Status: "pass"}
}

func checkHostCapacity() doctorCheck {
	// Reuse existing script logic via reading: do cheap probe, WARN only
	mem, _ := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
	if len(mem) == 0 {
		return doctorCheck{ID: "host.capacity", Name: "host capacity", Status: "skip", Fix: "scripts/check-host-capacity.sh"}
	}
	return doctorCheck{ID: "host.capacity", Name: "host capacity", Status: "pass", Version: strings.TrimSpace(string(mem)), Fix: "scripts/check-host-capacity.sh"}
}

func checkRegistryFresh() doctorCheck {
	if _, err := os.Stat(filepath.Join("docs", "unit-contracts.md")); err != nil {
		return doctorCheck{ID: "registry.fresh", Name: "unit-contracts freshness", Status: "warn", Fix: "go test ./internal/capabilities -run TestCapabilityContractsDocFresh -update"}
	}
	return doctorCheck{ID: "registry.fresh", Name: "unit-contracts freshness", Status: "pass"}
}

func checkDrift() doctorCheck {
	// Do not run templ/sqlc generate here (read-only). Warn if generated files predate .templ
	// Caller should run `gogogo generate --check` for the hard gate.
	return doctorCheck{ID: "generate.drift", Name: "generated drift (templ/sqlc)", Status: "skip", Required: "gogogo generate --check", Fix: "gogogo generate --check"}
}

func checkRecipes() doctorCheck {
	ids, err := recipe.ListReceipts(".")
	if err != nil {
		return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: "skip", Fix: "gogogo recipe check --format json"}
	}
	if len(ids) == 0 {
		return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: "skip", Fix: "gogogo recipe check --format json"}
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
			ID: "recipe.declarations", Name: "recipe declarations", Status: "fail",
			Version: fmt.Sprintf("%d checked", len(ids)),
			Fix:     "gogogo recipe check --format json",
		}
	}
	if warnings > 0 {
		return doctorCheck{
			ID: "recipe.declarations", Name: "recipe declarations", Status: "warn",
			Version: fmt.Sprintf("%d checked", len(ids)),
			Fix:     "gogogo recipe check --format json",
		}
	}
	return doctorCheck{ID: "recipe.declarations", Name: "recipe declarations", Status: "pass", Version: fmt.Sprintf("%d checked", len(ids))}
}

func jsonMarshal(v any) ([]byte, error) {
	// Thin wrapper so doctor.go does not pull encoding/json everywhere
	return marshalJSON(v)
}

// generate.go below
