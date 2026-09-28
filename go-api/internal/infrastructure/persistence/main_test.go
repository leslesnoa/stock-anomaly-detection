package persistence_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stock-anomaly-detection/go-api/migrations"
)

// TestMain はDATABASE_URLが設定されている場合（統合テスト実行時）、パッケージ内の
// どのテストよりも先にマイグレーションを適用する。これによりCIの手動psqlステップが不要になる。
// -short実行時（DATABASE_URL未設定）はスキップし、既存の各テスト内testing.Short()チェックに委ねる。
func TestMain(m *testing.M) {
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if err := persistence.RunMigrations(dbURL, migrations.FS); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
