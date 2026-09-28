package persistence

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// RunMigrations はfsysに埋め込まれた*.up.sql/*.down.sqlをdatabaseURLに適用する。
// golang-migrateはdatabase/sqlの*sql.DBを要求するため、Connect（pgxpool）とは別経路で接続する。
// pgxmigrate.WithInstanceはアドバイザリロック用の専用コネクションを内部に保持するため、
// 呼び出し側でsqlDBを直接Closeするのではなく、必ずm.Close()で両方まとめて解放する。
func RunMigrations(databaseURL string, fsys embed.FS) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db for migration: %w", err)
	}

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		sqlDB.Close()
		return fmt.Errorf("migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			log.Printf("WARN migrate close: source=%v db=%v", srcErr, dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
