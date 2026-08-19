package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockTokenService struct{ mock.Mock }

func (m *mockTokenService) IssueToken(userID string) (string, error) {
	args := m.Called(userID)
	return args.String(0), args.Error(1)
}
func (m *mockTokenService) VerifyToken(token string) (string, error) {
	args := m.Called(token)
	return args.String(0), args.Error(1)
}

func TestLoginUserUsecase_Handle_Success(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)
	tokens := new(mockTokenService)
	stored := user.User{ID: "user-1", Email: "a@example.com", PasswordHash: "hashed"}
	users.On("FindByEmail", mock.Anything, "a@example.com").Return(stored, nil)
	hasher.On("Verify", "hashed", "correct-pw").Return(nil)
	tokens.On("IssueToken", "user-1").Return("signed-token", nil)

	uc := usecase.NewLoginUserUsecase(users, hasher, tokens)
	token, err := uc.Handle(context.Background(), "a@example.com", "correct-pw")

	require.NoError(t, err)
	require.Equal(t, "signed-token", token)
}

func TestLoginUserUsecase_Handle_UserNotFound(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)
	tokens := new(mockTokenService)
	users.On("FindByEmail", mock.Anything, "missing@example.com").Return(user.User{}, user.ErrNotFound)

	uc := usecase.NewLoginUserUsecase(users, hasher, tokens)
	_, err := uc.Handle(context.Background(), "missing@example.com", "any-pw")

	require.ErrorIs(t, err, usecase.ErrInvalidCredentials)
}

func TestLoginUserUsecase_Handle_WrongPassword(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)
	tokens := new(mockTokenService)
	stored := user.User{ID: "user-1", Email: "a@example.com", PasswordHash: "hashed"}
	users.On("FindByEmail", mock.Anything, "a@example.com").Return(stored, nil)
	hasher.On("Verify", "hashed", "wrong-pw").Return(errors.New("mismatch"))

	uc := usecase.NewLoginUserUsecase(users, hasher, tokens)
	_, err := uc.Handle(context.Background(), "a@example.com", "wrong-pw")

	require.ErrorIs(t, err, usecase.ErrInvalidCredentials)
}
