package user

import (
	"context"
	"errors"
)

var ErrEmailAlreadyExists = errors.New("email already exists")
var ErrNotFound = errors.New("user not found")

type Repository interface {
	Create(ctx context.Context, u User) error
	FindByEmail(ctx context.Context, email string) (User, error)
}
