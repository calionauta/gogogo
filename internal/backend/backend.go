// SCOPE:layer=infra,removal=core - Backend abstraction for coherent backend stack
package backend

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

const (
	PB  = "pb"
	Alt = "alt"
)

type Backend interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	// App access for compatibility during migration
	PocketBase() core.App // may be nil for non-pb backends
}

type Config struct {
	Type string
}

func New(cfg Config) (Backend, error) {
	switch cfg.Type {
	case Alt, "alt-stack":
		return NewAltBackend()
	default:
		return NewPBBackend()
	}
}
