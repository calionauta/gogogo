package todo_test

import (
	"context"
	"testing"

	todomodel "github.com/calionauta/gogogo/features/todo"
	todoalt "github.com/calionauta/gogogo/features/todo/internal/repos/alt"
)

func TestContract_TodoRepo_AltTursoStyle(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := tmp + "/alt-test.db"

	repo, err := todoalt.NewTodoRepoSQLCWithConfig(dbPath, "", "")
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	if repo != nil {
		defer repo.Close()
	}

	ctx := context.Background()
	ownerID := "owner-turso"
	created, err := repo.Create(ctx, todomodel.Todo{Title: "test"}, ownerID, "idem-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Title != "test" {
		t.Fatalf("title mismatch")
	}

	got, err := repo.List(ctx, ownerID, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}
