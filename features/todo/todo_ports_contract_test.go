package todo_test

import (
	"context"
	"testing"

	todomodel "github.com/calionauta/gogogo/features/todo"
	todobackend "github.com/calionauta/gogogo/features/todo/internal/backend"
	todoalt "github.com/calionauta/gogogo/features/todo/internal/repos/alt"
	todopb "github.com/calionauta/gogogo/features/todo/internal/repos/pb"
)

func TestContract_TodoRepo_PB(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := buildFixture(t, nil)
	defer cleanup()

	_ = base
	repo := todopb.NewTodoRepo(app)

	ctx := context.Background()
	ownerID := "owner1"
	created, err := repo.Create(ctx, todomodel.Todo{Title: "buy milk"}, ownerID, "idem-pb-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Title != "buy milk" {
		t.Fatalf("title mismatch: %q", created.Title)
	}

	got, err := repo.List(ctx, ownerID, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}

	// Backend flag should exist (pb is default)
	_ = todobackend.Get() != todobackend.BackendPB
}

func TestContract_TodoRepo_Alt(t *testing.T) {
	t.Parallel()
	repo := todoalt.NewTodoRepo()
	ctx := context.Background()
	ownerID := "owner1"

	created, err := repo.Create(ctx, todomodel.Todo{Title: "buy milk"}, ownerID, "idem-alt-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Title != "buy milk" {
		t.Fatalf("title mismatch: %q", created.Title)
	}

	got, err := repo.List(ctx, ownerID, "all")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}
