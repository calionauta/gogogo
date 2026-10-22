// SCOPE:core — contract tests for the test-value guard itself.
//
// A gate that matches nothing is indistinguishable from a gate that is
// broken. Each case below pins one classification the guard must make, so a
// refactor that silences it fails here instead of failing open in CI.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "case_test.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func classes(findings []finding) map[string]string {
	out := map[string]string{}
	for _, f := range findings {
		out[f.test] = f.class
	}
	return out
}

func TestVacuousHasNoAssertion(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func TestNothing(t *testing.T) {
	doSomething()
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if classes(got)["Nothing"] != "vacuous" {
		t.Errorf("want vacuous, got %v", classes(got))
	}
}

func TestErrOnlyWarns(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func TestErrOnly(t *testing.T) {
	x, err := f()
	if err != nil {
		t.Fatal(err)
	}
	_ = x
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if classes(got)["ErrOnly"] != "err-only" {
		t.Errorf("want err-only, got %v", classes(got))
	}
}

func TestWireSymptomIsSpecific(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func TestStatus(t *testing.T) {
	code, err := f()
	if err != nil {
		t.Fatal(err)
	}
	if code != 200 {
		t.Errorf("status = %d, want 200", code)
	}
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestWantErrorContractIsSpecific(t *testing.T) {
	t.Parallel()
	// `if err == nil → t.Fatal` pins the failure mode. It is the negative
	// contract, not plumbing, so it must not warn.
	dir := writeTestFile(t, `package p

import "testing"

func TestWantError(t *testing.T) {
	_, err := f()
	if err == nil {
		t.Fatal("want error, got nil")
	}
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestHelperSinkCounts(t *testing.T) {
	t.Parallel()
	// A test that delegates its pins to an asserting helper must not be
	// flagged: resolution is by what the helper does (it fails the test),
	// not by its name.
	dir := writeTestFile(t, `package p

import "testing"
import "strings"

func assertStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("status = %d, want %d", got, want)
	}
}

func TestViaHelper(t *testing.T) {
	rec := f()
	assertStatus(t, rec, 200)
}

func TestOnlySetupHelper(t *testing.T) {
	// Known hole, pinned: a helper that only sets up (fails on setup
	// error) counts as a sink. Setup-only tests are obvious in review;
	// wrong predicates are not, and this gate aims at the second.
	f := newFixture(t)
	_ = f
}

func newFixture(t *testing.T) int {
	t.Helper()
	x, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	return x
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestTransitiveHelperCounts(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func waitFor(t *testing.T, ch <-chan int) {
	t.Helper()
	select {
	case v := <-ch:
		if v != 1 {
			t.Errorf("got %d", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func waitReady(t *testing.T, ch <-chan int) {
	t.Helper()
	waitFor(t, ch)
}

func TestTransitive(t *testing.T) {
	waitReady(t, ch)
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestBlankParamAndGoleakExempt(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func TestRaceOnly(_ *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); race() }()
	wg.Wait()
}

func TestLeakChecked(t *testing.T) {
	defer goleak.VerifyNone(t)
	exercise()
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestNestedRunCounts(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

func TestNested(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		if got := f(); got != 1 {
			t.Errorf("got %d", got)
		}
	})
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestNonBehaviorFuncsExcluded(t *testing.T) {
	t.Parallel()
	// TestMain, Examples (Output: is the assertion) and Benchmarks pin no
	// behavior and must stay silent even with zero failure calls.
	dir := writeTestFile(t, `package p

import "testing"

func TestMain(m *testing.M) { m.Run() }

func ExampleX() {}

func BenchmarkY(b *testing.B) { f() }
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

func TestDotRootIsWalked(t *testing.T) {
	t.Parallel()
	// Regression: invoked as `check-test-value .`, the walk root's own name
	// starts with a dot. Skipping it prunes the whole tree and the gate
	// passes vacuously — the exact failure this test pins.
	base := t.TempDir()
	root := filepath.Join(base, ".work")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "case_test.go"), []byte(`package p

import "testing"

func TestDotRoot(t *testing.T) {
	doSomething()
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := scanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if classes(got)["DotRoot"] != "vacuous" {
		t.Errorf("want vacuous (root walked), got %v", classes(got))
	}
}

func TestTestifyShapes(t *testing.T) {
	t.Parallel()
	dir := writeTestFile(t, `package p

import "testing"

import "github.com/stretchr/testify/assert"

func TestOnlyNoError(t *testing.T) {
	err := f()
	assert.NoError(t, err)
}

func TestEqual(t *testing.T) {
	assert.Equal(t, 200, code())
}
`)
	got, err := scanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := classes(got)
	if c["OnlyNoError"] != "err-only" {
		t.Errorf("OnlyNoError: want err-only, got %v", c)
	}
	if _, ok := c["Equal"]; ok {
		t.Errorf("Equal: want no finding, got %v", c)
	}
}
