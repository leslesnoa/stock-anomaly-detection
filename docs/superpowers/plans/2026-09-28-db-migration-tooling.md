# DBマイグレーション自動化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** go-api起動時（および統合テストのTestMain内）にgolang-migrateでマイグレーションを自動適用し、「デプロイ前の手動適用忘れ」という構造的リスクを排除する。

**Architecture:** `github.com/golang-migrate/migrate/v4` + pgx v5用DBドライバ(`database/pgx/v5`) + `embed.FS`ソース(`source/iofs`)。マイグレーションファイルを`.up.sql`/`.down.sql`ペアにリネームし、`go-api/migrations/embed.go`でバイナリに埋め込む。`internal/infrastructure/persistence`に新規`RunMigrations`関数を追加し、`main.go`の`persistence.Connect`直前と、`persistence`パッケージの`TestMain`から呼ぶ。CIの手動`psql`ステップは削除する。

**Tech Stack:** Go 1.24, golang-migrate/migrate v4, jackc/pgx v5 (`database/sql`ドライバとして`pgx/v5/stdlib`をblank import)

**Spec:** `docs/superpowers/specs/2026-09-28-db-migration-tooling-design.md`

## Global Constraints

- Go 1.24.0（go-api/go.mod、計画作成時点）。新規依存は`go get`/`go mod tidy`で解決し、バージョン番号を手で書かない — その結果`go.mod`の`go`ディレクティブ自体が引き上がる場合はそれに従う（Task 2実施時に`golang-migrate v4.20.1`の要求により`1.25.11`へ上がった。Task 4でCIの`actions/setup-go`を`go-version-file`方式に変更して追随させる）。
- 全SQL（up/down問わず）はPostgres 16対応で、`IF EXISTS`/`IF NOT EXISTS`により冪等に書く。
- 統合テストは既存方針を踏襲: `testing.Short()`または`DATABASE_URL`未設定でスキップする。
- 既存3マイグレーションのDDL内容（up側）は一切変更しない。リネームのみ（`git mv`で内容が変わらないことを保証する）。
- ロールバックCLI・アプリケーションからのdown実行は作らない（down.sqlファイル自体は用意するが、呼び出すコードは書かない）。

## Review Focus

- **docker-composeの`docker-entrypoint-initdb.d`は`*.sql`をファイル名の昇順（asciibetical）に実行するため、同じ番号内では`NNN_xxx.down.sql`が`NNN_xxx.up.sql`より先に実行される**（"down" < "up"）。down.sqlが`IF EXISTS`ガード無しだと、まだ存在しないテーブル/カラムへのDROPでコンテナ初期化が失敗する。→ Task 1の全down.sqlに`IF EXISTS`を必須で付ける。
- **本番は`schema_migrations`テーブルが存在しない状態で、3テーブル・1カラムが既に手動適用済み**（設計書ロールアウト節）。この状態に対して`RunMigrations`を実行してもエラーにならないことを自動テストで固定する。→ Task 2の`TestRunMigrations_AgainstManuallyAppliedSchema`。
- **golang-migrateの`pgx.WithInstance`はアドバイザリロック用に専用の`*sql.Conn`を追加で保持する**ため、`RunMigrations`が`*sql.DB`を直接`Close()`すると、そのロック用コネクションが確実には解放されない。→ Task 2で`m.Close()`を使う（設計書のイラスト用コードから修正）。
- **マイグレーション失敗時にサーバーが誤ったスキーマのまま起動しないこと**（`log.Fatalf`によるプロセス終了）は、既存の`mustEnv`失敗時と同じ検証不能な起動時Fatalパスであり、このコードベースの既存パターンでも自動テストされていない。→ 新規に単体テストは追加せず、コードレビューで確認する（Task 3）。
- **CI統合テストの実行順序**: `persistence`パッケージの`TestMain`が同パッケージの全テストより先にマイグレーションを適用することはGoのテストフレームワークが保証するため、他パッケージ（`watchlist_repository_test.go`等、`stock_name`列に依存）向けの追加の順序制御は不要。→ 設計の前提として明記するのみ（Task 4）。

---

## Task 1: マイグレーションファイルのレイアウト変更とembed.go追加

**Files:**
- Rename (via `git mv`, content unchanged): `go-api/migrations/001_initial_schema.sql` → `go-api/migrations/001_initial_schema.up.sql`
- Rename (via `git mv`, content unchanged): `go-api/migrations/002_add_watchlist_stock_name.sql` → `go-api/migrations/002_add_watchlist_stock_name.up.sql`
- Rename (via `git mv`, content unchanged): `go-api/migrations/003_create_daily_prices.sql` → `go-api/migrations/003_create_daily_prices.up.sql`
- Create: `go-api/migrations/001_initial_schema.down.sql`
- Create: `go-api/migrations/002_add_watchlist_stock_name.down.sql`
- Create: `go-api/migrations/003_create_daily_prices.down.sql`
- Create: `go-api/migrations/embed.go`

**Interfaces:**
- Produces: `migrations.FS` (`embed.FS`, package `github.com/stock-anomaly-detection/go-api/migrations`) — 6つの`*.sql`ファイルを埋め込む。Task 2以降で`persistence.RunMigrations(databaseURL, migrations.FS)`として消費される。

- [ ] **Step 1: 既存3ファイルを`git mv`でリネームする**

```bash
cd go-api/migrations
git mv 001_initial_schema.sql 001_initial_schema.up.sql
git mv 002_add_watchlist_stock_name.sql 002_add_watchlist_stock_name.up.sql
git mv 003_create_daily_prices.sql 003_create_daily_prices.up.sql
cd ../..
```

`git mv`はファイル内容を変更しない。これによりDDL内容の変更なしを保証する（手で新規ファイルを作って中身をコピーしない）。

- [ ] **Step 2: `go-api/migrations/embed.go`を作成する**

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 3: `go-api/migrations/001_initial_schema.down.sql`を作成する**

```sql
-- docker-composeのdocker-entrypoint-initdb.dは*.sqlをファイル名の昇順(asciibetical)に実行するため、
-- 同じ番号内では "down" < "up" によりこのファイルが001_initial_schema.up.sqlより先に実行される。
-- 初回起動時にまだテーブルが存在しない状態でDROPが走っても失敗しないよう、必ずIF EXISTSを使う。
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS watchlist;
DROP TABLE IF EXISTS users;
```

- [ ] **Step 4: `go-api/migrations/002_add_watchlist_stock_name.down.sql`を作成する**

```sql
-- 001_initial_schema.down.sqlと同じ理由でIF EXISTSを使う。
ALTER TABLE IF EXISTS watchlist DROP COLUMN IF EXISTS stock_name;
```

- [ ] **Step 5: `go-api/migrations/003_create_daily_prices.down.sql`を作成する**

```sql
-- 001_initial_schema.down.sqlと同じ理由でIF EXISTSを使う。
DROP TABLE IF EXISTS daily_prices;
```

- [ ] **Step 6: ファイル一覧を確認する**

Run: `ls go-api/migrations/`
Expected: `.gitkeep`, `001_initial_schema.up.sql`, `001_initial_schema.down.sql`, `002_add_watchlist_stock_name.up.sql`, `002_add_watchlist_stock_name.down.sql`, `003_create_daily_prices.up.sql`, `003_create_daily_prices.down.sql`, `embed.go` の8ファイル。

- [ ] **Step 7: Commit**

```bash
git add go-api/migrations/
git commit -m "$(cat <<'EOF'
feat(go-api): lay out migrations for golang-migrate

Renames the 3 existing migration files to golang-migrate's .up.sql
convention (content unchanged via git mv), adds matching .down.sql
files, and embeds them into the binary via embed.go. No DDL changes.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `persistence.RunMigrations`の実装（TDD）

**Files:**
- Create: `go-api/internal/infrastructure/persistence/migrate.go`
- Create: `go-api/internal/infrastructure/persistence/migrate_test.go`
- Modify: `go-api/go.mod`, `go-api/go.sum`（`go mod tidy`による自動更新）

**Interfaces:**
- Consumes: `migrations.FS`（Task 1で作成、`embed.FS`）
- Produces: `func persistence.RunMigrations(databaseURL string, fsys embed.FS) error` — Task 3（`main.go`）とTask 4（`main_test.go`）から呼ばれる。

- [ ] **Step 1: 失敗するテストを先に書く — `go-api/internal/infrastructure/persistence/migrate_test.go`**

```go
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
```

- [ ] **Step 2: テストが失敗する（コンパイルエラー）ことを確認する**

Run: `cd go-api && go vet ./internal/infrastructure/persistence/...`
Expected: FAIL — `persistence.RunMigrations` は未定義、`github.com/stock-anomaly-detection/go-api/migrations`パッケージも未定義（Task 1で作成済みのはずだが、この時点では`RunMigrations`が無いためビルドが通らない）。

- [ ] **Step 3: 依存を追加する**

```bash
cd go-api
go get github.com/golang-migrate/migrate/v4
go mod tidy
```

これにより`go.mod`/`go.sum`に`github.com/golang-migrate/migrate/v4`とその間接依存が解決したバージョンで追記される。バージョン番号は`go get`が解決した値をそのまま使う（手で書き換えない）。

- [ ] **Step 4: `go-api/internal/infrastructure/persistence/migrate.go`を実装する**

```go
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
```

- [ ] **Step 5: テストを実行して通ることを確認する**

ローカルにPostgresが必要（`docker-compose up -d db`等で起動済みのものでよい。テストは専用の使い捨てDBを作るため、既存データへの影響はない）。

Run:
```bash
cd go-api
DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test -race -run TestRunMigrations ./internal/infrastructure/persistence/... -v
```

Expected: `TestRunMigrations_FreshDatabase`, `TestRunMigrations_IdempotentOnSecondRun`, `TestRunMigrations_AgainstManuallyAppliedSchema` すべてPASS。

- [ ] **Step 6: Commit**

```bash
git add go-api/go.mod go-api/go.sum go-api/internal/infrastructure/persistence/migrate.go go-api/internal/infrastructure/persistence/migrate_test.go
git commit -m "$(cat <<'EOF'
feat(go-api): add persistence.RunMigrations via golang-migrate

Adds a dedicated migration-runner function separate from Connect
(golang-migrate requires database/sql's *sql.DB, not pgxpool).
Tests cover a fresh database, idempotent re-runs, and — matching the
design spec's rollout note — re-running against a database where the
schema was already applied manually with no schema_migrations table.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `main.go`への組み込み

**Files:**
- Modify: `go-api/cmd/api/main.go`

**Interfaces:**
- Consumes: `persistence.RunMigrations(databaseURL string, fsys embed.FS) error`（Task 2）、`migrations.FS`（Task 1）

- [ ] **Step 1: importを追加する**

`go-api/cmd/api/main.go`の既存import群（4行目〜19行目）に以下を追加する:

```go
	"github.com/stock-anomaly-detection/go-api/migrations"
```

配置場所: 既存の`"github.com/stock-anomaly-detection/go-api/internal/usecase"`の直後（`"internal/..."` < `"migrations"` のアルファベット順で最後になる）。import順序はビルド結果に影響しないため、`gofmt`/`goimports`済みであれば厳密な位置は問わない。

- [ ] **Step 2: `RunMigrations`呼び出しを挿入する**

`go-api/cmd/api/main.go`の該当箇所を書き換える:

変更前（65-66行目）:
```go
	databaseURL := mustEnv("DATABASE_URL")
	pool, err := persistence.Connect(ctx, databaseURL)
```

変更後:
```go
	databaseURL := mustEnv("DATABASE_URL")
	if err := persistence.RunMigrations(databaseURL, migrations.FS); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}
	pool, err := persistence.Connect(ctx, databaseURL)
```

サーバーはマイグレーション成功後にのみ起動する。失敗時は既存の`mustEnv`・DB接続失敗時と同じ「起動時に即座に失敗する」方針（`log.Fatalf`）に揃える。

- [ ] **Step 3: ビルド確認**

Run: `cd go-api && go build ./...`
Expected: エラーなくビルドが通る。

- [ ] **Step 4: Commit**

```bash
git add go-api/cmd/api/main.go
git commit -m "$(cat <<'EOF'
feat(go-api): run migrations at startup before serving traffic

Wires persistence.RunMigrations into main.go immediately after
DATABASE_URL is read and before Connect, so the server never starts
against a stale schema. A migration failure is fatal, matching the
existing mustEnv/Connect failure pattern.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: CI統合テストへの組み込みと手動ステップ削除

**Files:**
- Create: `go-api/internal/infrastructure/persistence/main_test.go`
- Modify: `.github/workflows/go-test.yml`

**Interfaces:**
- Consumes: `persistence.RunMigrations`（Task 2）、`migrations.FS`（Task 1）

- [ ] **Step 1: `go-api/internal/infrastructure/persistence/main_test.go`を作成する**

```go
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
```

- [ ] **Step 2: ローカルで統合テスト一式を実行して確認する**

Run:
```bash
cd go-api
DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test -race ./... -v 2>&1 | tail -60
```

Expected: `persistence`パッケージの既存リポジトリテスト（`user_repository_test.go`等、`stock_name`列やdaily_pricesテーブルに依存するものを含む）がすべてPASSする。`TestMain`が最初にマイグレーションを適用しているため、手動でのスキーマ準備は不要。

- [ ] **Step 3: `.github/workflows/go-test.yml`から手動マイグレーションステップを削除する**

変更前（38-54行目）:
```yaml
      - name: Run unit tests
        working-directory: go-api
        run: go test -race -short ./...

      - name: Run database migration
        run: |
          psql $DATABASE_URL -f go-api/migrations/001_initial_schema.sql
          psql $DATABASE_URL -f go-api/migrations/002_add_watchlist_stock_name.sql
          psql $DATABASE_URL -f go-api/migrations/003_create_daily_prices.sql
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable

      - name: Run integration tests
        working-directory: go-api
        run: go test -race ./...
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

変更後:
```yaml
      - name: Run unit tests
        working-directory: go-api
        run: go test -race -short ./...

      - name: Run integration tests
        working-directory: go-api
        run: go test -race ./...
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

- [ ] **Step 4: `actions/setup-go`のバージョン指定を`go-version-file`に切り替える**

**背景（Task 2で判明、コントローラーのルーリング）:** Task 2で`go get github.com/golang-migrate/migrate/v4 && go mod tidy`を実行した結果、`golang-migrate/migrate/v4 v4.20.1`自身の`go.mod`が`go 1.25.11`を要求するため、`go-api/go.mod`の`go`ディレクティブが`1.24.0`から`1.25.11`へ自動的に引き上げられている（Task 2で確認済み・許容済み。バージョン番号を手で書かないというGlobal Constraintsに従った結果であり、正しい）。
`.github/workflows/go-test.yml`は`go-version: '1.24'`を固定指定しているため、このままでは実際にビルドに使われるGoバージョン（go.modが要求する1.25.11、`GOTOOLCHAIN=auto`により自動ダウンロードされる）とワークフローの表記が食い違う。今後go.modの`go`ディレクティブが変わるたびにこのファイルを追随させる手間を無くすため、固定バージョン指定ではなく`go-version-file`でgo.modから読み取る方式に変更する。

`go-api/go.mod`の該当行:
```
go 1.25.11
```
（このファイルはTask 2で既に更新済み。このTaskでは変更しない。）

変更前（`.github/workflows/go-test.yml`、`actions/setup-go`ステップ）:
```yaml
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true
          cache-dependency-path: go-api/go.sum
```

変更後:
```yaml
      - uses: actions/setup-go@v5
        with:
          go-version-file: go-api/go.mod
          cache: true
          cache-dependency-path: go-api/go.sum
```

- [ ] **Step 5: Commit**

```bash
git add go-api/internal/infrastructure/persistence/main_test.go .github/workflows/go-test.yml
git commit -m "$(cat <<'EOF'
ci(go-api): apply migrations via TestMain instead of manual psql step

persistence.RunMigrations is now the only place DATABASE_URL-backed
tests get their schema from. Removes the CI step that manually ran
psql per migration file — a step that must be remembered on every new
migration, which is exactly what caused the PR #20 incident.

Also switches actions/setup-go to go-version-file (reading go-api/go.mod)
instead of a hardcoded go-version. golang-migrate v4.20.1 itself requires
go 1.25.11, which go mod tidy already raised go.mod to in the previous
commit; go-version-file keeps CI in sync with go.mod automatically
instead of needing a manual bump every time a dependency raises the
language version floor.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: ドキュメント更新

**Files:**
- Modify: `docs/deployment/phase5-railway-vercel-setup.md`（4節）
- Modify: `CLAUDE.md`（CI Actionsの節）

- [ ] **Step 1: `docs/deployment/phase5-railway-vercel-setup.md`の4節を書き換える**

変更前（73-108行目、見出しから次の見出し直前まで）:
```markdown
## 4. DBマイグレーションの適用

RailwayのPostgresプラグインの`DATABASE_URL`は通常`*.railway.internal`を指しており、
開発者のローカル端末からは名前解決できない。そのため`railway connect`でRailwayのプロキシ
経由のローカルpsqlセッションを開き、その中でマイグレーションファイルを読み込む。

Railway CLIでプロジェクトにリンクした状態で実行する（Postgresプラグインのサービス名は
慣例的に`Postgres`と大文字始まりであることに注意）:

```bash
railway link
railway connect Postgres
```

`railway connect`が開いたpsqlセッションの中で、`go-api/migrations/`ディレクトリの
マイグレーションファイルを、ファイル名の昇順に実行する。番号がついた順序で実行することで、
将来マイグレーションファイルが追加されたとき、このドキュメントを更新する手間を省ける:

```
\i go-api/migrations/001_initial_schema.sql
\i go-api/migrations/002_add_watchlist_stock_name.sql
\i go-api/migrations/003_create_daily_prices.sql
```

> **既にRedisプラグインを追加済みの環境について:** go-apiは2026-09-24以降Redisを一切参照しない。
> Railwayプロジェクトに残っているRedisプラグインと`REDIS_URL`の変数参照は削除してよい。

> **既存環境のアップグレード手順（2026-09-24）:** `003_create_daily_prices.sql` は
> **新しいgo-apiイメージをデプロイする前に**適用すること。テーブルが無い状態でgo-apiが起動すると、
> 起動時バックフィルが全銘柄で失敗し、そのプロセスの間は再試行されない。
> デプロイ後に適用してしまった場合は、go-apiサービスを再起動して起動時バックフィルをやり直す。
> 適用後は以下で各銘柄におよそ500行（約2年分）入っていることを確認する:
>
> ```sql
> SELECT stock_code, count(*), min(date), max(date) FROM daily_prices GROUP BY stock_code;
> ```
```

変更後:
```markdown
## 4. DBマイグレーションの適用

2026-09-28以降、マイグレーションはgo-api起動時に自動適用される（golang-migrate、
`internal/infrastructure/persistence/migrate.go`の`RunMigrations`）。手動でのpsql実行は不要。
マイグレーション適用に失敗した場合、go-apiは起動せずプロセスが終了する（`log.Fatalf`）ため、
誤ったスキーマのままサービスが立ち上がることはない。

新しいマイグレーションファイルを追加する場合は、`go-api/migrations/`に
`NNN_xxx.up.sql`/`NNN_xxx.down.sql`のペアで追加すればよい。デプロイ順序に関する
特別な注意（旧版で必要だった「新イメージのデプロイ前に適用」等）は不要になった。

> **既にRedisプラグインを追加済みの環境について:** go-apiは2026-09-24以降Redisを一切参照しない。
> Railwayプロジェクトに残っているRedisプラグインと`REDIS_URL`の変数参照は削除してよい。

緊急時に手動でスキーマ状態を確認・介入する場合は、`railway connect Postgres`でPostgresへ
接続し、以下のクエリでgolang-migrateの適用状態を確認できる:

```sql
SELECT * FROM schema_migrations;
SELECT stock_code, count(*), min(date), max(date) FROM daily_prices GROUP BY stock_code;
```
```

- [ ] **Step 2: `CLAUDE.md`のCI Actions節を書き換える**

変更前:
```markdown
## GitHub Actions CI
- postgres:16 サービス
- `DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`
- マイグレーションは `go-api/migrations/*.sql` をファイル名指定で順に適用する。**新しいマイグレーションを追加したら `.github/workflows/go-test.yml` の "Run database migration" ステップにも追記すること**
```

変更後:
```markdown
## GitHub Actions CI
- postgres:16 サービス
- `DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`
- マイグレーションはgo-api起動時（および`persistence`パッケージの`TestMain`）にgolang-migrateで自動適用される。新しいマイグレーションを追加する場合は `go-api/migrations/` に `NNN_xxx.up.sql`/`NNN_xxx.down.sql` のペアで追加すればよく、CIワークフローへの追記は不要
```

- [ ] **Step 3: Commit**

```bash
git add docs/deployment/phase5-railway-vercel-setup.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: update deploy guide and CLAUDE.md for automatic migrations

Both documented the old manual-apply workflow that this change
removes. Reflects that go-api now applies migrations at startup and
that CI no longer needs a per-file manual step.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## 最終確認（全タスク完了後）

- [ ] `cd go-api && go test -race -short ./...` がPASSする
- [ ] `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable go test -race ./...` がPASSする
- [ ] `cd go-api && go build ./...` が通る
- [ ] `git status` がクリーンであること（全変更がコミット済み）
- [ ] CLAUDE.mdのワークフローに従い、push & PR作成を行う（ローカルマージ・保留のまま放置しない）
