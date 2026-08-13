package persistence_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/require"
)

func setupUserTestDB(t *testing.T) (context.Context, *persistence.PgUserRepository, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := persistence.Connect(ctx, databaseURL)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "TRUNCATE users CASCADE")
	require.NoError(t, err)
	return ctx, persistence.NewPgUserRepository(conn), func() { conn.Close() }
}

func TestPgUserRepository_CreateAndFindByEmail(t *testing.T) {
	ctx, repo, cleanup := setupUserTestDB(t)
	defer cleanup()

	err := repo.Create(ctx, user.User{Email: "alice@example.com", PasswordHash: "hashed"})
	require.NoError(t, err)

	got, err := repo.FindByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", got.Email)
	require.Equal(t, "hashed", got.PasswordHash)
	require.NotEmpty(t, got.ID)
	require.False(t, got.CreatedAt.IsZero())
}

func TestPgUserRepository_Create_DuplicateEmail(t *testing.T) {
	ctx, repo, cleanup := setupUserTestDB(t)
	defer cleanup()

	require.NoError(t, repo.Create(ctx, user.User{Email: "bob@example.com", PasswordHash: "h1"}))
	err := repo.Create(ctx, user.User{Email: "bob@example.com", PasswordHash: "h2"})
	require.Error(t, err)
	require.True(t, errors.Is(err, user.ErrEmailAlreadyExists))
}

func TestPgUserRepository_FindByEmail_NotFound(t *testing.T) {
	ctx, repo, cleanup := setupUserTestDB(t)
	defer cleanup()

	_, err := repo.FindByEmail(ctx, "nobody@example.com")
	require.Error(t, err)
	require.True(t, errors.Is(err, user.ErrNotFound))
}
