package installer

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// findUnit returns the manifest unit with id, failing the test otherwise.
func findUnit(t *testing.T, id string) trimUnit {
	t.Helper()
	for _, u := range manifestUnits {
		if u.id == id {
			return u
		}
	}
	t.Fatalf("no manifest unit %q", id)
	return trimUnit{}
}

// captureStdout runs fn with the process stdout replaced by a pipe and
// returns what it wrote. The old apply_test.go copy of this helper died with
// the trim engine; advise and scaffold tests still need it.
func captureStdout(t *testing.T, fn func(w *os.File)) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(r)
		done <- string(raw)
	}()
	fn(w)
	_ = w.Close()
	return <-done
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// repoRoot returns the repository root of this checkout from this file's
// own position, so a test that needs a pristine template path never shells
// to git.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
