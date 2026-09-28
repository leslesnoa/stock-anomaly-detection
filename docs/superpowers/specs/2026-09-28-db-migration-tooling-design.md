# DBマイグレーション自動化 設計書

## 背景・動機

`go-api/migrations/`のマイグレーション適用は、本番（Railway CLI経由の手動`psql`実行）・CI（`.github/workflows/go-test.yml`へのファイル名手動追記）・ローカル（docker-composeの`docker-entrypoint-initdb.d`、既存ボリュームには反映されない）の3箇所でバラバラに、かつ手動で管理されている。

2026-09-27〜28、この構造が直接の原因となる本番インシデントが発生した: PR #20で`003_create_daily_prices.sql`を追加したが、「デプロイ前に手動でマイグレーションを適用する」という運用ルールが実際には守られず、`003`未適用のまま新しいgo-apiイメージが本番にデプロイされた。結果、`daily_prices`テーブルが存在せず、起動時バックフィルが監視対象全銘柄で失敗し、異常検知が実質停止する障害となった（[[project_status]]参照）。この種の「デプロイとスキーマ適用が別々の手動ステップに分離しており、順序を間違えると本番が壊れる」という構造的リスクを解消する。

マイグレーションファイルは現在3つ（`001`〜`003`）。既存の[[project_followup_db_migration_tooling]]フォローアップで「ファイルが増えたら自動化ツール導入を検討する」としていたが、今回のインシデントで前倒しで着手する。

## ゴール

- マイグレーション適用をgo-apiの起動シーケンスに組み込み、「デプロイ前に手動でマイグレーションを当て忘れる」という失敗モードを構造的に排除する
- CIの統合テストも同じ経路で自動的にスキーマを最新化し、`.github/workflows/go-test.yml`への手動追記運用をなくす
- マイグレーションファイルの追加・適用順序の管理をツール（golang-migrate）に委譲する

## 非ゴール

- ロールバック（down migration実行）の運用コマンド・CLIは作らない。down migrationファイル自体はgolang-migrateの規約上必要なため用意するが、実行するのは緊急時に開発者が手元で`migrate` CLIを使う場合のみを想定し、アプリケーションからは呼ばない
- 既存3ファイルの中身（DDL）は一切変更しない。リネームとdown migration追加のみ
- ローカルdocker-composeの`docker-entrypoint-initdb.d`マウント自体は変更しない（go-api起動時の自動適用により、既存ボリュームでも次回go-api起動時に追いつくようになるのは副次効果であり、docker-compose.yml自体への変更は不要）

## アーキテクチャ

### 使用ライブラリ

- `github.com/golang-migrate/migrate/v4`
- `github.com/golang-migrate/migrate/v4/database/pgx/v5`（pgx v5用データベースドライバ。内部で`database/sql`の`*sql.DB`を要求するため、`github.com/jackc/pgx/v5/stdlib`をblank importして`"pgx"`ドライバを登録する）
- `github.com/golang-migrate/migrate/v4/source/iofs`（`embed.FS`をマイグレーションのソースとして扱う）

### マイグレーションファイルのレイアウト

`go-api/migrations/`配下を以下のように変更する:

```
go-api/migrations/
  embed.go                              # 新規: //go:embed *.sql
  001_initial_schema.up.sql             # リネーム（中身は現行001_initial_schema.sqlのまま）
  001_initial_schema.down.sql           # 新規
  002_add_watchlist_stock_name.up.sql   # リネーム（中身は現行002_add_watchlist_stock_name.sqlのまま）
  002_add_watchlist_stock_name.down.sql # 新規
  003_create_daily_prices.up.sql        # リネーム（中身は現行003_create_daily_prices.sqlのまま）
  003_create_daily_prices.down.sql      # 新規
```

`embed.go`:

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

down migrationの内容（reverse DDL、既存のCREATE/ALTER文の対称）:

- `001_initial_schema.down.sql`: `notifications`・`watchlist`テーブルをDROP後、`users`をDROP（外部キー依存の逆順）
- `002_add_watchlist_stock_name.down.sql`: `watchlist.stock_name`列をDROP
- `003_create_daily_prices.down.sql`: `daily_prices`テーブルをDROP

### コード変更

`internal/infrastructure/persistence/`に新規関数を追加する（既存の`Connect`とは別関数。golang-migrateは`*pgxpool.Pool`ではなく独自の`*sql.DB`を要求するため、責務を分離する）:

```go
// internal/infrastructure/persistence/migrate.go
package persistence

func RunMigrations(databaseURL string, fsys embed.FS) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db for migration: %w", err)
	}
	defer sqlDB.Close()

	driver, err := pgx5migrate.WithInstance(sqlDB, &pgx5migrate.Config{})
	if err != nil {
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

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
```

`cmd/api/main.go`: `databaseURL := mustEnv("DATABASE_URL")`の直後、`persistence.Connect(...)`より前に呼ぶ:

```go
databaseURL := mustEnv("DATABASE_URL")
if err := persistence.RunMigrations(databaseURL, migrations.FS); err != nil {
	log.Fatalf("failed to run migrations: %v", err)
}
pool, err := persistence.Connect(ctx, databaseURL)
```

サーバーはマイグレーション成功後にのみ起動する。失敗時は`log.Fatalf`でプロセスを終了する（誤ったスキーマのまま起動時バックフィル等が走ることを防ぐ）。

### CI・テストへの組み込み

`internal/infrastructure/persistence/`の全ての統合テストは`persistence.Connect`を直接呼んでおり（`main.go`経由ではない）、この経路だけが`DATABASE_URL`を使う唯一の場所であることを確認済み（`go-api`内で`DATABASE_URL`を参照するテストファイルは`persistence`パッケージのみ）。そこで同パッケージに`TestMain`を追加する:

```go
// internal/infrastructure/persistence/main_test.go
package persistence_test

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

`-short`実行時（`DATABASE_URL`未設定）はマイグレーションをスキップし、既存の各テスト内`testing.Short()`チェックがそれらをスキップする現行動作は変わらない。

`.github/workflows/go-test.yml`の「Run database migration」ステップ（`psql`でファイルを1つずつ適用）を削除する。統合テストステップ実行時に`persistence`パッケージの`TestMain`が自動的にマイグレーションを適用する。

### ドキュメント更新

`docs/deployment/phase5-railway-vercel-setup.md`の4節「DBマイグレーションの適用」を、「go-api起動時に自動適用されるため手動操作は不要」という内容に書き換える。あわせて、デプロイ順序に関する注意書き（「003は新しいgo-apiイメージをデプロイする前に適用すること」等）を削除する（この制約自体が解消されるため）。緊急時に手動でスキーマ状態を確認・介入する場合のための`psql`コマンド例（`SELECT * FROM schema_migrations;`等）は残す。

`CLAUDE.md`の「新しいマイグレーションを追加したら`.github/workflows/go-test.yml`の"Run database migration"ステップにも追記すること」という一文を削除し、「マイグレーションファイルを`go-api/migrations/`に`NNN_xxx.up.sql`/`NNN_xxx.down.sql`のペアで追加すれば、起動時に自動適用される」という説明に置き換える。

## エラーハンドリング

- マイグレーション適用失敗（SQL構文エラー、DB接続不可等）: `main.go`は`log.Fatalf`でプロセスを終了する。既存の`mustEnv`・DB接続失敗時と同じ「起動時に即座に失敗する」方針に揃える
- `migrate.ErrNoChange`（適用すべき新規マイグレーションが無い場合）はエラー扱いしない
- CIの`TestMain`でのマイグレーション失敗もテストスイート全体を即座に失敗させる（`os.Exit(1)`）

## テスト方針

- `persistence.RunMigrations`自体の単体テスト: `testing.Short()`でスキップする統合テストとして追加し、（a）新規データベースに対して3件のマイグレーションが適用されテーブルが作成されること、（b）2回連続実行しても`ErrNoChange`によりエラーにならず冪等であること、を検証する
- 既存の`persistence`パッケージの統合テスト（`user_repository_test.go`等）は`TestMain`によるマイグレーション自動適用を前提にできるため、変更不要
- リネーム後の`.up.sql`ファイルの中身がリネーム前と完全に一致すること（DDLの変更なし）を実装時に確認する

## ロールアウト

このリポジトリは`main`マージで即座に本番へ自動デプロイされる構成（Railway/Vercel）。マージ後、次のgo-api起動時に`RunMigrations`が走るが、対象の3マイグレーションは全て本番に適用済み（`schema_migrations`相当のテーブルがまだ存在しないため、golang-migrateが初回起動時に`schema_migrations`管理テーブルを作成し、3件を「新規適用」として実行しようとする点に注意）。

**重要な既存状態との整合:** 本番Postgresには既に`users`・`watchlist`・`notifications`・`daily_prices`の4テーブルと`watchlist.stock_name`列が存在する（過去の手動適用により）。golang-migrateはこれらのテーブルが既に存在する状態を認識しないため、素朴に`m.Up()`を実行すると`CREATE TABLE IF NOT EXISTS`により実害はない（各DDLが`IF NOT EXISTS`/`ADD COLUMN IF NOT EXISTS`で書かれているため）が、`schema_migrations`テーブルには「まだ何も適用していない」という初期状態から始まり、001〜003を実際に（冪等に）再実行して`schema_migrations`に記録する形になる。これは安全（各DDLが冪等）だが、実装タスクの中で本番相当のデータが入ったPostgresに対して実際にこの初回実行を手元で検証し、エラーなく完了することを確認するステップを含める。
