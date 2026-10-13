// SCOPE:core — Host gate verdict tests (pure logic, instant).
package hostgate

import (
	"strings"
	"testing"
)

func healthy() (Stats, Thresholds) {
	return Stats{MemAvailMB: 8192, Cores: 4, Load5: 1.0, DiskFreeMB: 20480},
		Thresholds{MinRAMMB: 2048, MaxLoadPerCore: 2.0, MinDiskMB: 1024}
}

func TestEvaluateHealthyPasses(t *testing.T) {
	s, th := healthy()
	if ok, report := Evaluate(s, th); !ok {
		t.Fatalf("healthy box refused: %s", report)
	}
}

func TestEvaluateNamesEachFailedDimension(t *testing.T) {
	s, th := healthy()
	s.MemAvailMB = 512
	s.Load5 = 20
	s.DiskFreeMB = 10
	ok, report := Evaluate(s, th)
	if ok {
		t.Fatal("saturated box passed")
	}
	for _, want := range []string{"RAM 512", "20.00", "disk 10"} {
		if !strings.Contains(report, want) {
			t.Errorf("report %q must name %q (measured numbers, not labels)", report, want)
		}
	}
}

func TestEvaluateBoundaryPasses(t *testing.T) {
	s, th := healthy()
	s.MemAvailMB = th.MinRAMMB
	s.DiskFreeMB = th.MinDiskMB
	s.Load5 = th.MaxLoadPerCore * float64(s.Cores)
	if ok, report := Evaluate(s, th); !ok {
		t.Fatalf("exact-threshold box refused (must be >=, not >): %s", report)
	}
}
