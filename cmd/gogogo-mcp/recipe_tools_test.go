package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeToolsRequireDirAndConfirm(t *testing.T) {
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"install", func() error {
			_, _, err := handleRecipeInstall(context.Background(), nil, recipeArgs{Src: "/tmp/recipe"})
			return err
		}},
		{"update", func() error {
			_, _, err := handleRecipeUpdate(context.Background(), nil, recipeArgs{Src: "/tmp/recipe"})
			return err
		}},
		{"uninstall", func() error {
			_, _, err := handleRecipeUninstall(context.Background(), nil, recipeArgs{ID: "pomodoro"})
			return err
		}},
		{"status", func() error { _, _, err := handleRecipeStatus(context.Background(), nil, recipeArgs{}); return err }},
		{"outdated", func() error {
			_, _, err := handleRecipeOutdated(context.Background(), nil, recipeArgs{})
			return err
		}},
	} {
		if err := tt.call(); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("%s without dir: expected required-fields refusal, got %v", tt.name, err)
		}
	}
}

func TestRecipeToolsDryRunReachesTheEngine(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "rbac", "casbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "rbac", "casbin", "policy.csv"), []byte("p, admin, /*, .*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "web", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const manifest = "id: pomodoro\ndeclaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n"
	if err := os.WriteFile(filepath.Join(root, ".recipe.yml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleRecipeInstall(context.Background(), nil, recipeArgs{
		Src: root, Dir: root, DryRun: true, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleRecipeInstall: %v", err)
	}
	if text := toolText(t, res); !strings.Contains(text, "pomodoro") {
		t.Errorf("plan does not name the recipe: %s", text)
	}
	if _, statErr := os.Stat(filepath.Join(root, "features")); !os.IsNotExist(statErr) {
		t.Error("dry run copied files — it must change nothing")
	}
	_, _, err = handleRecipeUpdate(context.Background(), nil, recipeArgs{
		Src: root, Dir: root, DryRun: true, Format: "json",
	})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("update without a receipt must refuse, got %v", err)
	}
}

func TestRecipeStatusReadsAnEmptyCheckout(t *testing.T) {
	dir := t.TempDir()
	// Smallest template checkout `looksLikeTemplate` accepts: the two
	// marker files and an empty receipts dir. No receipt means "nothing
	// installed yet", which is a success answer, not a refusal.
	for _, f := range []struct{ path, body string }{
		{"cmd/web/main.go", "package main\n"},
		{"internal/installer/run.go", "package installer\n"},
		{"go.mod", "module example.com/demo\n\ngo 1.27\n"},
	} {
		full := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleRecipeStatus(context.Background(), nil, recipeArgs{
		Dir: dir, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleRecipeStatus: %v", err)
	}
	if text := toolText(t, res); !strings.Contains(text, "nothing is installed yet") {
		t.Errorf("empty checkout status changed meaning: %s", text)
	}
}

// TestRecipeOutdatedVerdictIsNotAnError mirrors the version_status rule for
// the recipe half: "behind" arrives as exit 3 and must still deliver its
// JSON instead of becoming a tool error.
func TestRecipeOutdatedVerdictIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct{ path, body string }{
		{"cmd/web/main.go", "package main\n"},
		{"internal/installer/run.go", "package installer\n"},
		{"go.mod", "module example.com/demo\n\ngo 1.27\n"},
	} {
		full := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".recipes", "pomodoro.yml"), []byte(
		"id: pomodoro\nversion: v1.2.0\nupstream: {repo: example.com/recipes, ref: v1.2.0}\nfiles: {}\ndeclaration: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(dir, "catalog", "pomodoro")
	if err := os.MkdirAll(catalog, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, "v1.3.0.yaml"), []byte(
		"id: pomodoro\nsummary: A timer.\nversion: v1.3.0\nsource: {repo: example.com/recipes, ref: v1.3.0}\n"+
			"files: {features/pomodoro/handlers/chi.go: sha256:"+strings.Repeat("a", 64)+"}\n"+
			"license: MIT\nminCore: v0.56.0\nupstreamDeps: []\n"+
			"reviewedAt: \"2026-11-14\"\nreviewedBy: calionauta\nreviewedAgainst: 3f8a1c2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, _, err := handleRecipeOutdated(context.Background(), nil, recipeArgs{Dir: dir})
	if err != nil {
		t.Fatalf("a verdict must not surface as a tool error: %v", err)
	}
	var rep struct {
		Outdated int `json:"outdated"`
		Results  []struct {
			ID        string `json:"id"`
			Installed string `json:"installed"`
			Latest    string `json:"latest"`
			Status    string `json:"status"`
		} `json:"results"`
	}
	if jerr := json.Unmarshal([]byte(toolText(t, res)), &rep); jerr != nil {
		t.Fatalf("recipe_outdated must answer JSON: %v", jerr)
	}
	if rep.Outdated != 1 || len(rep.Results) != 1 || rep.Results[0].Status != "behind" {
		t.Fatalf("expected one behind row, got %+v", rep)
	}
}

func TestRecipeListDescribesBuiltIns(t *testing.T) {
	res, _, err := handleRecipeList(context.Background(), nil, recipeListArgs{
		Dir: filepath.Join("..", ".."), Format: "json",
	})
	if err != nil {
		t.Fatalf("recipe list: %v", err)
	}
	text := toolText(t, res)
	for _, want := range []string{`"todo"`, `"kind":"feature"`, `"origin":"capability"`} {
		if !strings.Contains(text, want) {
			t.Errorf("recipe list missing %s: %s", want, text[:1000])
		}
	}
	describe, _, err := handleRecipeDescribe(context.Background(), nil, recipeDescribeArgs{
		ID: "dashboard", Dir: filepath.Join("..", ".."), Format: "json",
	})
	if err != nil {
		t.Fatalf("recipe describe: %v", err)
	}
	if text := toolText(t, describe); !strings.Contains(text, "dashboard") || !strings.Contains(text, "feature") {
		t.Errorf("describe output misses id/kind: %s", text)
	}
	// The agent-facing half: a verb name alone is not choosable. The todo
	// receipt is the reference shape and must surface its description and
	// per-param help through the same tool.
	todo, _, err := handleRecipeDescribe(context.Background(), nil, recipeDescribeArgs{
		ID: "todo", Dir: filepath.Join("..", ".."), Format: "json",
	})
	if err != nil {
		t.Fatalf("recipe describe todo: %v", err)
	}
	todoText := toolText(t, todo)
	for _, want := range []string{"invokeDescriptions", "Create a todo", "invokeParamHelp", "idem_key"} {
		if !strings.Contains(todoText, want) {
			t.Errorf("todo describe must carry %q: %s", want, todoText)
		}
	}
}

func TestRecipeInvokeForwardsToLoopback(t *testing.T) {
	var gotPath string
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if c, err := r.Cookie("gogogo_auth"); err == nil {
			gotCookie = c.Value
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("invoked ok"))
	}))
	defer srv.Close()

	res, _, err := handleRecipeInvoke(context.Background(), nil, recipeInvokeArgs{
		ID: "todo", Verb: "create", Server: srv.URL, Session: "tok-7",
		Params: map[string]string{"title": "Via MCP"}, Dir: filepath.Join("..", ".."), Format: "json",
	})
	if err != nil {
		t.Fatalf("recipe invoke: %v", err)
	}
	if text := toolText(t, res); !strings.Contains(text, "invoked ok") {
		t.Errorf("invoke output missing server body: %s", text)
	}
	if gotPath != "/api/ext/invoke" {
		t.Errorf("path = %s, want /api/ext/invoke", gotPath)
	}
	if gotCookie != "tok-7" {
		t.Errorf("cookie = %q, want tok-7", gotCookie)
	}
}
