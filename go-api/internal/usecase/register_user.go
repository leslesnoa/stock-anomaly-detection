package usecase

import (
	"context"
	"errors"
	"net/mail"

	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
)

var ErrInvalidEmail = errors.New("invalid email format")
var ErrPasswordTooShort = errors.New("password must be at least 8 characters")

const minPasswordLength = 8

type RegisterUserUsecase struct {
	users  user.Repository
	hasher auth.PasswordHasher
}

func NewRegisterUserUsecase(users user.Repository, hasher auth.PasswordHasher) *RegisterUserUsecase {
	return &RegisterUserUsecase{users: users, hasher: hasher}
}

func (u *RegisterUserUsecase) Handle(ctx context.Context, email, password string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	if len(password) < minPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := u.hasher.Hash(password)
	if err != nil {
		return err
	}
	return u.users.Create(ctx, user.User{Email: email, PasswordHash: hash})
}
