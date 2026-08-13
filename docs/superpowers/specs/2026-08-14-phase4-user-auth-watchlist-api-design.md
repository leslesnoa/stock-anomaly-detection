# Phase 4 バックエンド: ユーザー管理・JWT認証・watchlist API 設計書

## 1. 背景・スコープ

設計書 `docs/superpowers/specs/2026-07-05-stock-anomaly-detection-design.md` の
Phase 4「ユーザー管理・JWT認証・Next.js UI」を、バックエンドとフロントエンドの2サブプロジェクトに分割する
（フロントエンドは別途ブレインストーミングする）。本設計はバックエンド部分のみを対象とする。

**スコープに含むもの**:
- ユーザー登録（`POST /auth/register`）・ログイン（`POST /auth/login`、JWT発行）
- JWT認証ミドルウェア
- watchlist CRUD API（一覧・追加・削除・閾値変更）

**スコープに含まないもの（フォローアップ）**:
- Next.js フロントエンド（別サブプロジェクト）
- 監視ループ（`MonitorUsecase`）をDBの`watchlist`テーブル駆動に切り替える対応。
  現状どおり環境変数`STOCK_CODES`の固定銘柄リストを監視し続ける。
  watchlist APIで登録した銘柄は、この対応が入るまで実際の監視・通知には反映されない。

DBスキーマ（`users`・`watchlist`テーブル）は`go-api/migrations/001_initial_schema.sql`に既に存在し、
`watchlist`の読み取り（`FindByUserID`）も実装済みのため、新規マイグレーションは不要。

## 2. 全体アーキテクチャ

```
main.go
├── goroutine: http.ListenAndServe(":"+PORT, mux)   ← 新規
└── monitor.StartMonitoring(ctx, codes, ...)         ← 既存（変更なし）

mux（net/http.ServeMux, Go 1.22+ メソッド+パスパターン構文）
├── POST /auth/register  → authHandler.Register
├── POST /auth/login     → authHandler.Login
├── GET    /watchlist     → authMiddleware(watchlistHandler.List)
├── POST   /watchlist     → authMiddleware(watchlistHandler.Add)
├── DELETE /watchlist/{id}→ authMiddleware(watchlistHandler.Remove)
└── PATCH  /watchlist/{id}→ authMiddleware(watchlistHandler.UpdateThreshold)
```

- `PORT`は新規環境変数（デフォルト`8080`。Railwayは`PORT`を自動注入する）
- 既存の監視ループとDB接続（`pgx.Conn`）・Redis接続は共有して使い回す
- 同一プロセス内でHTTPサーバーと監視ループを並行起動する（別サービスには分離しない）
- 依存方向はClean Architectureのまま: `interface/handler` → `usecase` → `domain` ← `infrastructure/persistence`
- ルーティングは外部ルーターを追加せず標準ライブラリ`net/http`のみで実装する

## 3. ルーティング選定の理由

| 案 | 内容 | 判断 |
|---|---|---|
| **標準ライブラリ net/http（採用）** | Go 1.22+の拡張ServeMux（メソッド+パスパターン）のみで実装 | プロジェクトの「シンプルさ優先」方針（DIにWireを使わない等）に合致。追加依存ゼロ |
| chi router | 軽量な外部ルーター | ミドルウェアやパラメータ抽出が書きやすいが新規依存が増える。今回の規模では不要と判断 |

認証方式は以下の3案を検討し、JWTミドルウェアパターンを採用する。

| 案 | 内容 | 判断 |
|---|---|---|
| **JWTミドルウェアパターン（採用）** | `Authorization: Bearer <token>`をミドルウェアで検証し`user_id`をcontextに埋め込む | Goの標準的なイディオム。設計書7章の「JWT・Bearerヘッダー必須」要件に合致 |
| ハンドラ内で個別にトークン検証 | 各ハンドラの先頭で毎回パース | 保護対象エンドポイントが増えるたびにコード重複 → 不採用 |
| Redisセッションベース認証 | JWTの代わりにRedisにセッションを保存 | 設計書がJWTを明記。認証にRedis依存を追加すると結合が増える → 不採用 |

## 4. domain層

### `internal/domain/user/`（新規）

```go
type User struct {
    ID              string
    Email           string
    PasswordHash    string
    SlackWebhookURL *string
    CreatedAt       time.Time
}

type Repository interface {
    Create(ctx context.Context, u User) error
    FindByEmail(ctx context.Context, email string) (User, error)
}

var ErrEmailAlreadyExists = errors.New("email already exists")
var ErrNotFound = errors.New("user not found")
```

- `SlackWebhookURL`は`users`テーブルに既存のカラムだが、現時点では未使用（監視ループ連携は上記フォローアップ）
- `Create`はメール重複時に`ErrEmailAlreadyExists`を返す
- `FindByEmail`は未存在時に`ErrNotFound`を返す

### `internal/domain/watchlist/`（既存の`repository.go`を拡張）

```go
type Repository interface {
    FindByUserID(ctx context.Context, userID string) ([]Watchlist, error) // 既存・変更なし
    Create(ctx context.Context, w Watchlist) (Watchlist, error)           // 新規
    Delete(ctx context.Context, id, userID string) error                  // 新規
    UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error // 新規
}

var ErrAlreadyExists = errors.New("stock already in watchlist")
var ErrNotFound = errors.New("watchlist item not found")
```

- `Delete`・`UpdateThreshold`は`WHERE id=$1 AND user_id=$2`で対象を絞り込み、所有者チェックを兼ねる。
  0件更新の場合は`ErrNotFound`を返す（「存在しない」と「他ユーザーの登録」を区別せずどちらも404扱いにするため）

### `internal/domain/auth/`（新規）

パスワードハッシュ化とJWT発行は外部ライブラリに依存するため、CLAUDE.mdの依存ルール
「usecaseはdomainのみに依存」に従い、domain層にインターフェースを新設して抽象化する
（既存の`notifier.Notifier` ↔ `SlackClient`と同じパターン）。

```go
type PasswordHasher interface {
    Hash(password string) (string, error)
    Verify(hash, password string) error
}

type TokenService interface {
    IssueToken(userID string) (string, error)
    VerifyToken(token string) (userID string, err error)
}

var ErrInvalidToken = errors.New("invalid token")
```

## 5. usecase層

### `usecase/register_user.go`

- email形式（`net/mail.ParseAddress`）・パスワード最小長（8文字）を検証
- `hasher.Hash` → `users.Create`。メール重複時は`user.ErrEmailAlreadyExists`をそのまま返す

### `usecase/login_user.go`

- `users.FindByEmail` → `hasher.Verify` → 成功なら`tokens.IssueToken(user.ID)`
- 「メール不在」と「パスワード不一致」はどちらも共通の`ErrInvalidCredentials`にまとめる
  （メールの存在有無を外部に漏らさないため）

### `usecase/manage_watchlist.go`

- `Add(ctx, userID, stockCode, threshold)` — `stock.NewStockCode`で銘柄コード検証、
  閾値未指定（0）時は`2.5`をデフォルト。重複登録は`watchlist.ErrAlreadyExists`
- `Remove(ctx, userID, watchlistID)` — 所有者チェック込みで削除
- `UpdateThreshold(ctx, userID, watchlistID, threshold)` — 所有者チェック込みで更新
- `List(ctx, userID)` — 既存の`FindByUserID`をそのまま利用

## 6. interface層

### `interface/handler/auth_handler.go`

| エンドポイント | リクエスト | 成功時 | エラー時 |
|---|---|---|---|
| `POST /auth/register` | `{email, password}` | `201` | メール重複`409`、バリデーション`400` |
| `POST /auth/login` | `{email, password}` | `200` + `{"token": "..."}` | 認証失敗`401` |

### `interface/handler/auth_middleware.go`

- `Authorization: Bearer <token>`を検証（欠落・不正フォーマット・検証失敗は全て`401`）
- 検証成功時、`user_id`を非公開のcontext key型でリクエストcontextに埋め込み次のハンドラへ

### `interface/handler/watchlist_handler.go`（すべて`authMiddleware`配下）

| エンドポイント | リクエスト | 成功時 | エラー時 |
|---|---|---|---|
| `GET /watchlist` | - | `200` + JSON配列 | - |
| `POST /watchlist` | `{stock_code, alert_threshold}` | `201` | 重複`409`、不正な銘柄コード`400` |
| `DELETE /watchlist/{id}` | - | `204` | 不在/他ユーザー`404` |
| `PATCH /watchlist/{id}` | `{alert_threshold}` | `200` | 不在/他ユーザー`404` |

エラーレスポンスは全ハンドラ共通で`{"error": "<message>"}`のJSON形式に統一する。

## 7. infrastructure層

### `infrastructure/persistence/user_repository.go`（新規）

```go
type PgUserRepository struct{ conn *pgx.Conn }
func (r *PgUserRepository) Create(ctx context.Context, u user.User) error
    // UNIQUE制約違反（pgconn code 23505）→ user.ErrEmailAlreadyExists
func (r *PgUserRepository) FindByEmail(ctx context.Context, email string) (user.User, error)
    // pgx.ErrNoRows → user.ErrNotFound
```

### `infrastructure/persistence/watchlist_repository.go`（既存ファイルを拡張）

- `Create`（UNIQUE制約違反 → `watchlist.ErrAlreadyExists`）
- `Delete`／`UpdateThreshold`（`RowsAffected()==0` → `watchlist.ErrNotFound`）

### `interface/gateway/bcrypt_hasher.go`・`interface/gateway/jwt_token_service.go`（新規）

- bcrypt（`bcrypt.DefaultCost`）
- JWT（`golang-jwt/jwt/v5`、HS256、`exp`=発行から24時間後、`sub`=userID、鍵は環境変数`JWT_SECRET`）

## 8. main.go 配線

- 新規環境変数: `JWT_SECRET`（必須）、`PORT`（任意・デフォルト`8080`）
- 各コンポーネントをDI組み立てし、`go func() { http.ListenAndServe(":"+port, mux) }()`を
  既存の`monitor.StartMonitoring(...)`呼び出しの前に追加する（監視ループ側の挙動は変更しない）
- CLAUDE.mdの環境変数一覧に`JWT_SECRET`・`PORT`を追記する

## 9. テスト方針

| 層 | 方針 |
|---|---|
| `usecase/*` | `testify/mock`で`user.Repository`/`auth.PasswordHasher`/`auth.TokenService`/`watchlist.Repository`をモック（既存パターン踏襲） |
| `infrastructure/persistence/*` | 統合テスト。`testing.Short()`または環境変数未設定でスキップ、実DB使用（既存`notification_repository_test.go`踏襲） |
| `interface/gateway/bcrypt_hasher_test.go`・`jwt_token_service_test.go` | 外部依存なしの純粋ユニットテスト |
| `interface/handler/*` | `httptest`でハンドラ単体テスト、usecaseはモック |
| CI | `go test -race -short ./...`が通ること（既存ワークフローに変更不要） |

## 10. エラーハンドリング方針まとめ

| シナリオ | 対応 |
|---|---|
| メール重複登録 | `409 Conflict` |
| ログイン失敗（メール不在／パスワード不一致） | `401 Unauthorized`（原因を区別せず統一） |
| JWT欠落・不正・期限切れ | `401 Unauthorized` |
| 銘柄コード不正 | `400 Bad Request` |
| watchlist重複登録 | `409 Conflict` |
| watchlist不在／他ユーザーの登録操作 | `404 Not Found`（区別せず統一） |
