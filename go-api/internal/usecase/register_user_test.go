package usecase_test

import (
	"context"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockUserRepository struct{ mock.Mock }

func (m *mockUserRepository) Create(ctx context.Context, u user.User) error {
	return m.Called(ctx, u).Error(0)
}
func (m *mockUserRepository) FindByEmail(ctx context.Context, email string) (user.User, error) {
	args := m.Called(ctx, email)
	return args.Get(0).(user.User), args.Error(1)
}

type mockPasswordHasher struct{ mock.Mock }

func (m *mockPasswordHasher) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}
func (m *mockPasswordHasher) Verify(hash, password string) error {
	return m.Called(hash, password).Error(0)
}

func TestRegisterUserUsecase_Handle_Success(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)
	hasher.On("Hash", "password123").Return("hashed-password", nil)
	users.On("Create", mock.Anything, user.User{Email: "new@example.com", PasswordHash: "hashed-password"}).Return(nil)

	uc := usecase.NewRegisterUserUsecase(users, hasher)
	err := uc.Handle(context.Background(), "new@example.com", "password123")

	require.NoError(t, err)
	users.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestRegisterUserUsecase_Handle_InvalidEmail(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)

	uc := usecase.NewRegisterUserUsecase(users, hasher)
	err := uc.Handle(context.Background(), "not-an-email", "password123")

	require.ErrorIs(t, err, usecase.ErrInvalidEmail)
	users.AssertNotCalled(t, "Create")
}

func TestRegisterUserUsecase_Handle_PasswordTooShort(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)

	uc := usecase.NewRegisterUserUsecase(users, hasher)
	err := uc.Handle(context.Background(), "new@example.com", "short")

	require.ErrorIs(t, err, usecase.ErrPasswordTooShort)
	users.AssertNotCalled(t, "Create")
}

func TestRegisterUserUsecase_Handle_DuplicateEmail(t *testing.T) {
	users := new(mockUserRepository)
	hasher := new(mockPasswordHasher)
	hasher.On("Hash", "password123").Return("hashed-password", nil)
	users.On("Create", mock.Anything, mock.Anything).Return(user.ErrEmailAlreadyExists)

	uc := usecase.NewRegisterUserUsecase(users, hasher)
	err := uc.Handle(context.Background(), "dup@example.com", "password123")

	require.ErrorIs(t, err, user.ErrEmailAlreadyExists)
}
