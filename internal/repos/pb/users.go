// SCOPE:feature,removal=plugin - PB-backed users repo adapter (POC)
package pb

import (
	"context"

	"github.com/calionauta/gogogo/internal/ports"
)

type UsersRepo struct{}

func NewUsersRepo() *UsersRepo { return &UsersRepo{} }

func (r *UsersRepo) GetByID(ctx context.Context, id string) (*ports.User, error) {
	_ = ctx
	_ = id
	return &ports.User{ID: id}, nil
}

func (r *UsersRepo) GetByEmail(ctx context.Context, email string) (*ports.User, error) {
	_ = ctx
	_ = email
	return &ports.User{Email: email}, nil
}

func (r *UsersRepo) Create(ctx context.Context, u *ports.User) error {
	_ = ctx
	_ = u
	return nil
}

func (r *UsersRepo) Update(ctx context.Context, u *ports.User) error {
	_ = ctx
	_ = u
	return nil
}

var _ ports.UserRepo = (*UsersRepo)(nil)
