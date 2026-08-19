package usecase

import (
	"context"
	"errors"

	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type LoginUserUsecase struct {
	users  user.Repository
	hasher auth.PasswordHasher
	tokens auth.TokenService
}

func NewLoginUserUsecase(users user.Repository, hasher auth.PasswordHasher, tokens auth.TokenService) *LoginUserUsecase {
	return &LoginUserUsecase{users: users, hasher: hasher, tokens: tokens}
}

func (u *LoginUserUsecase) Handle(ctx context.Context, email, password string) (string, error) {
	usr, err := u.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}
	if err := u.hasher.Verify(usr.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}
	return u.tokens.IssueToken(usr.ID)
}
