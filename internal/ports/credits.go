// SCOPE:feature,removal=plugin - Credits repo interface (POC)
package ports

import "context"

type CreditsRepo interface {
	GetBalance(ctx context.Context, userID string) (int64, error)
	Adjust(ctx context.Context, userID string, delta int64, reason string) error
}
