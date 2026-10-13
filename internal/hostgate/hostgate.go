// SCOPE:core — Host gate for heavy Go tests (skip on saturated boxes)
//
// Some tests ARE the heavy workload: they copy the whole repo into temp
// dirs and fork `go vet ./...` per copy (see
// internal/installer/trim_build_test.go, where N units each vet a full
// tree in parallel). On a small shared host that wedges the box — and
// co-tenant agents share these cores — so the test itself must refuse
// before scaffolding, exactly like `make ci-local` refuses via
// scripts/check-host-capacity.sh. A Makefile/script gate cannot cover a
// bare `go test -run ...` invocation; the in-test skip is the only layer
// that fires no matter who launched it.
//
// Contract: SkipIfConstrained skips (never fails) when this is NOT CI
// and the box is constrained, printing dimensions, thresholds and the
// fallback — so whatever reads the log (human or LLM) knows WHY nothing
// ran and what to do instead. On GitHub runners (CI=true) it never
// skips: CI is the full gate by design. CI_LOCAL_FORCE=1 overrides.
package hostgate

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Thresholds are the floors for ONE heavy test (repo copy + forked vet).
// Lighter than the T4 gate (whole suite + browsers): a single heavy test
// needs headroom, not a whole machine.
type Thresholds struct {
	// MinRAMMB refuses below this MemAvailable (MiB).
	MinRAMMB int
	// MaxLoadPerCore refuses above this 5-minute loadavg per core.
	MaxLoadPerCore float64
	// MinDiskMB refuses below this free space on the temp filesystem.
	MinDiskMB int
}

// Defaults keeps every knob overridable via env (HOSTGATE_MIN_RAM_MB,
// HOSTGATE_MAX_LOAD_PER_CORE, HOSTGATE_MIN_DISK_MB), same convention as
// scripts/check-host-capacity.sh.
const (
	defaultMinRAMMB       = 2048
	defaultMaxLoadPerCore = 2.0
	defaultMinDiskMB      = 1024
	bytesPerMB            = 1024
)

func Defaults() Thresholds {
	return Thresholds{
		MinRAMMB:       envInt("HOSTGATE_MIN_RAM_MB", defaultMinRAMMB),
		MaxLoadPerCore: envFloat("HOSTGATE_MAX_LOAD_PER_CORE", defaultMaxLoadPerCore),
		MinDiskMB:      envInt("HOSTGATE_MIN_DISK_MB", defaultMinDiskMB),
	}
}

// Stats is one measured sample of this box.
type Stats struct {
	MemAvailMB int
	Cores      int
	Load5      float64
	DiskFreeMB int
}

// Evaluate is the pure verdict (unit-tested): ok=false names the failed
// dimensions in report, always with the measured numbers.
func Evaluate(s Stats, th Thresholds) (ok bool, report string) {
	var bad []string
	if s.MemAvailMB < th.MinRAMMB {
		bad = append(bad, fmt.Sprintf("RAM %d MB avail < %d required",
			s.MemAvailMB, th.MinRAMMB))
	}
	perCore := 0.0
	if s.Cores > 0 {
		perCore = s.Load5 / float64(s.Cores)
	}
	if perCore > th.MaxLoadPerCore {
		bad = append(bad, fmt.Sprintf("load %.2f 5-min avg on %d cores (%.2f/core > %.2f) — box saturated",
			s.Load5, s.Cores, perCore, th.MaxLoadPerCore))
	}
	if s.DiskFreeMB < th.MinDiskMB {
		bad = append(bad, fmt.Sprintf("disk %d MB free on temp fs < %d required",
			s.DiskFreeMB, th.MinDiskMB))
	}
	if len(bad) == 0 {
		return true, ""
	}
	return false, strings.Join(bad, "; ")
}

// SkipIfConstrained skips the calling test when this box cannot carry a
// heavy test right now. what names the workload (e.g. "copies and vets
// the whole repo per unit") so the skip message is actionable. No-op on
// CI runners and under CI_LOCAL_FORCE=1.
func SkipIfConstrained(tb testing.TB, what string) {
	tb.Helper()
	if os.Getenv("CI") == "true" {
		return
	}
	if os.Getenv("CI_LOCAL_FORCE") == "1" {
		tb.Logf("hostgate: CI_LOCAL_FORCE=1, running heavy test anyway (%s)", what)
		return
	}
	th := Defaults()
	s, ok := Read()
	if !ok {
		// Unmeasurable box (non-Linux, no statfs): fail open with a log
		// line. Refusing on unknown evidence would silently halve CI
		// coverage on exotic runners; running is the reversible choice.
		tb.Logf("hostgate: host stats unavailable, running heavy test anyway (%s)", what)
		return
	}
	if ok, report := Evaluate(s, th); !ok {
		tb.Skipf("hostgate: box cannot carry this heavy test (%s): %s "+
			"[cores=%d load5=%.2f memAvail=%dMB diskFree=%dMB] — let the PR's CI run it, "+
			"or re-run when the load settles (override: CI_LOCAL_FORCE=1)",
			what, report, s.Cores, s.Load5, s.MemAvailMB, s.DiskFreeMB)
	}
}

// Read samples this box. ok=false when any dimension is unreadable.
func Read() (Stats, bool) {
	mem, okMem := memAvailMB()
	load, okLoad := load5()
	disk, okDisk := tmpFreeMB()
	cores := runtime.NumCPU()
	if !okMem || !okLoad || !okDisk || cores <= 0 {
		return Stats{}, false
	}
	return Stats{MemAvailMB: mem, Cores: cores, Load5: load, DiskFreeMB: disk}, true
}

func memAvailMB() (int, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		var v int
		if n, _ := fmt.Sscanf(line, "MemAvailable: %d", &v); n == 1 {
			return v / bytesPerMB, true
		}
	}
	return 0, false
}

func load5() (float64, bool) {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// tmpFreeMB reports free space on the filesystem backing os.TempDir —
// where t.TempDir() copies land — via statfs (no fork, Linux+darwin).
func tmpFreeMB() (int, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &st); err != nil {
		return 0, false
	}
	//nolint:gosec // G115: Bavail/Bsize are non-negative kernel counters;
	// their product fits uint64 on any real filesystem (terabytes, not 2^64
	// bytes), and the result is only compared against MB thresholds.
	return int(uint64(st.Bavail) * uint64(st.Bsize) / (bytesPerMB * bytesPerMB)), true
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
