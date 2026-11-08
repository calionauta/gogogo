package i18n

import "testing"

func TestBundledCatalogCoversTodoSlice(t *testing.T) {
	c := Default()
	pt, ok := c.Lookup("pt-BR", "todo.tasks")
	if !ok || pt != "Tarefas" {
		t.Fatalf("bundled pt-BR todo.tasks = %q, %v", pt, ok)
	}
	en, ok := c.Lookup("en", "todo.tasks")
	if !ok || en != "Tasks" {
		t.Fatalf("bundled en todo.tasks = %q, %v", en, ok)
	}
	for _, key := range []string{
		"todo.filter.all", "todo.filter.active", "todo.filter.completed",
		"todo.empty.title", "todo.empty.body",
	} {
		if _, ok := c.Lookup("pt-BR", key); !ok {
			t.Fatalf("bundled pt-BR missing %q", key)
		}
		if _, ok := c.Lookup("en", key); !ok {
			t.Fatalf("bundled en missing %q", key)
		}
	}
}
