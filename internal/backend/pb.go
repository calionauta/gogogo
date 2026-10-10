// SCOPE:layer=infra,removal=core - PocketBase backend implementation
package backend

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

type PBBackend struct {
	app core.App
}

func NewPBBackend() (Backend, error) {
	// For now, return a minimal stub - actual app setup happens elsewhere
	return &PBBackend{}, nil
}

func (b *PBBackend) Name() string {
	return PB
}

func (b *PBBackend) Start(_ context.Context) error {
	return nil
}

func (b *PBBackend) Stop() error {
	return nil
}

func (b *PBBackend) PocketBase() core.App {
	return b.app
}
