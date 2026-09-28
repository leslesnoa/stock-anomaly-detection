package persistence_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stock-anomaly-detection/go-api/migrations"
	"github.com/stretchr/testify/require"
)

// withFreshDatabase はbaseURLが指すPostgresサーバー上に使い捨てのデータベースを作成し、
// そのデータベースを指す接続文字列を返す。DATABASE_URLが指す共有DBはTestMain（Task 4）が
// 既にマイグレーション適用済みにしてしまうため、RunMigrations自体の「まっさらなDBへの初回適用」
// 「schema_migrationsが無い状態での再適用」を検証するには専用のDBが要る。
func withFreshDatabase(t *testing.T, baseURL string) string {
	t.Helper()

	adminDB, err := sql.Open("pgx", baseURL)
	require.NoError(t, err)
	defer adminDB.Close()

	dbName := fmt.Sprintf("migrate_test_%d", time.Now().UnixNano())
	_, err = adminDB.Exec("CREATE DATABASE " + dbName)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("pgx", baseURL)
		if err != nil {
			return
		}
		defer cleanupDB.Close()
		_, _ = cleanupDB.Exec("DROP DATABASE IF EXISTS " + dbName)
	})

	u, err := url.Parse(baseURL)
	require.NoError(t, err)
	u.Path = "/" + dbName
	return u.String()
}

func TestRunMigrations_FreshDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	dbURL := withFreshDatabase(t, baseURL)

	err := persistence.RunMigrations(dbURL, migrations.FS)
	require.NoError(t, err)

	sqlDB, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	defer sqlDB.Close()

	for _, table := range []string{"users", "watchlist", "notifications", "daily_prices"} {
		var exists bool
		err := sqlDB.QueryRow(
			"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists)
		require.NoError(t, err)
		require.Truef(t, exists, "table %s should exist after RunMigrations", table)
	}
}

func TestRunMigrations_IdempotentOnSecondRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	dbURL := withFreshDatabase(t, baseURL)

	require.NoError(t, persistence.RunMigrations(dbURL, migrations.FS))
	require.NoError(t, persistence.RunMigrations(dbURL, migrations.FS))
}

// TestRunMigrations_AgainstManuallyAppliedSchema は本番の実際の状況を再現する:
// schema_migrationsテーブルが存在しない状態で、001〜003のDDLが（golang-migrate経由ではなく）
// 手動で既に適用済みのデータベースに対してRunMigrationsを実行しても、各DDLが
// IF NOT EXISTS/IF EXISTSで書かれているためエラーにならないことを確認する
// （設計書のロールアウト節で要求されている検証を自動テスト化したもの）。
func TestRunMigrations_AgainstManuallyAppliedSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	dbURL := withFreshDatabase(t, baseURL)

	sqlDB, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	defer sqlDB.Close()

	for _, file := range []string{
		"001_initial_schema.up.sql",
		"002_add_watchlist_stock_name.up.sql",
		"003_create_daily_prices.up.sql",
	} {
		content, err := migrations.FS.ReadFile(file)
		require.NoError(t, err)
		_, err = sqlDB.Exec(string(content))
		require.NoError(t, err)
	}

	err = persistence.RunMigrations(dbURL, migrations.FS)
	require.NoError(t, err)
}
