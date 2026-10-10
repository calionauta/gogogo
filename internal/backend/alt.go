// SCOPE:layer=infra,removal=feature - Alt backend implementation (sqlc+tursogo)
package backend

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

type AltBackend struct{}

func NewAltBackend() (Backend, error) {
	return &AltBackend{}, nil
}

func (b *AltBackend) Name() string {
	return Alt
}

func (b *AltBackend) Start(_ context.Context) error {
	return nil
}

func (b *AltBackend) Stop() error {
	return nil
}

func (b *AltBackend) PocketBase() core.App {
	return nil
}
