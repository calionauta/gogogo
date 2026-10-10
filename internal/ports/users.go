// SCOPE:feature,removal=plugin - User repo interface (POC)
package ports

import "context"

type User struct {
	ID    string
	Email string
}

type UserRepo interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
}
