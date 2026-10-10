// SCOPE:feature,removal=feature - Todo backend selector (vertical slice)
package backend

import (
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/features/todo/internal/ports"
	todoalt "github.com/calionauta/gogogo/features/todo/internal/repos/alt"
	todopb "github.com/calionauta/gogogo/features/todo/internal/repos/pb"
)

const (
	BackendPB  = "pb"
	BackendAlt = "alt"
)

func Get() string {
	b := os.Getenv("DB_BACKEND")
	if b == "" {
		return BackendPB // default preserves behavior
	}
	return b
}

func GetFromConfig(cfgDBBackend string) string {
	if cfgDBBackend == "" {
		return BackendPB
	}
	return cfgDBBackend
}

func NewTodoRepo(b string, app core.App) ports.TodoRepo {
	switch b {
	case BackendAlt:
		return todoalt.NewTodoRepo()
	default:
		return todopb.NewTodoRepo(app)
	}
}

func NewTodoRepoWithConfig(b string, app core.App, dbPath string) ports.TodoRepo {
	switch b {
	case BackendAlt:
		r, err := todoalt.NewTodoRepoSQLC(dbPath)
		if err != nil {
			return todoalt.NewTodoRepo()
		}
		return r
	default:
		return todopb.NewTodoRepo(app)
	}
}
