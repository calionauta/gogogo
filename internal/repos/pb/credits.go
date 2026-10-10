// SCOPE:feature,removal=plugin - PB-backed credits repo adapter (POC)
package pb

import (
	"context"

	"github.com/calionauta/gogogo/internal/ports"
)

type CreditsRepo struct{}

func NewCreditsRepo() *CreditsRepo { return &CreditsRepo{} }

func (r *CreditsRepo) GetBalance(ctx context.Context, userID string) (int64, error) {
	// PB-backed implementation (preserves behavior in full impl)
	_ = ctx
	_ = userID
	return 0, nil
}

func (r *CreditsRepo) Adjust(ctx context.Context, userID string, delta int64, reason string) error {
	_ = ctx
	_ = userID
	_ = delta
	_ = reason
	return nil
}

var _ ports.CreditsRepo = (*CreditsRepo)(nil)
