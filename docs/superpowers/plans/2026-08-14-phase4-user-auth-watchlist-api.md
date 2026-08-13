# Phase 4 バックエンド（ユーザー管理・JWT認証・watchlist API）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ユーザー登録・JWTログイン・watchlist（監視銘柄）のCRUD APIをGoバックエンドに追加する。

**Architecture:** 既存のClean Architecture構成（`domain`→`usecase`→`interface`/`infrastructure`）を踏襲し、
`net/http`標準ライブラリのみでHTTPサーバーを構築する。パスワードハッシュ化とJWT発行はdomain層に
インターフェース（`auth.PasswordHasher`, `auth.TokenService`）を新設して抽象化し、実装は
`interface/gateway`に置く（既存の`notifier.Notifier` ↔ `SlackClient`と同じパターン）。
HTTPサーバーは既存の監視ループと同一プロセス内でgoroutineとして並行起動する。

**Tech Stack:** Go 1.24, `net/http`標準ライブラリ, `github.com/golang-jwt/jwt/v5`（新規追加）,
`golang.org/x/crypto/bcrypt`（既存の間接依存を直接利用）, `github.com/jackc/pgx/v5`,
`github.com/stretchr/testify`（mock/assert/require）。

参照仕様書: `docs/superpowers/specs/2026-08-14-phase4-user-auth-watchlist-api-design.md`

## Global Constraints

- Go 1.24（`go-api/go.mod`）
- 依存ルール: `domain`はGo標準ライブラリのみ使用／`usecase`は`domain`のみに依存／`interface`・`infrastructure`は`usecase`と`domain`に依存
- HTTPルーティングは`net/http`標準ライブラリのみ（外部ルーター追加禁止）
- JWTはHS256署名、有効期限は発行から24時間
- パスワードハッシュは`bcrypt.DefaultCost`を使用
- 全APIのエラーレスポンスは`{"error": "<message>"}`のJSON形式に統一
- 全テストで`-race`フラグ必須。統合テストは`testing.Short()`または`DATABASE_URL`未設定でスキップ
- コマンドは`go-api/`ディレクトリから実行: `go test -race -short ./...`、`go build ./...`
- DBスキーマ（`users`・`watchlist`テーブル）は`go-api/migrations/001_initial_schema.sql`に既存。新規マイグレーション不要
- 監視ループ（`MonitorUsecase`）のDB watchlist駆動化は本計画のスコープ外（別タスク）

---

### Task 1: user domain（エンティティ・リポジトリインターフェース）

**Files:**
- Create: `go-api/internal/domain/user/entity.go`
- Create: `go-api/internal/domain/user/repository.go`

**Interfaces:**
- Produces:
  - `type user.User struct { ID string; Email string; PasswordHash string; SlackWebhookURL *string; CreatedAt time.Time }`
  - `type user.Repository interface { Create(ctx context.Context, u User) error; FindByEmail(ctx context.Context, email string) (User, error) }`
  - `var user.ErrEmailAlreadyExists = errors.New("email already exists")`
  - `var user.ErrNotFound = errors.New("user not found")`

- [ ] **Step 1: entity.goを作成**

```go
package user

import "time"

type User struct {
	ID              string
	Email           string
	PasswordHash    string
	SlackWebhookURL *string
	CreatedAt       time.Time
}
```

- [ ] **Step 2: repository.goを作成**

```go
package user

import (
	"context"
	"errors"
)

var ErrEmailAlreadyExists = errors.New("email already exists")
var ErrNotFound = errors.New("user not found")

type Repository interface {
	Create(ctx context.Context, u User) error
	FindByEmail(ctx context.Context, email string) (User, error)
}
```

- [ ] **Step 3: ビルド確認**

Run（`go-api/`ディレクトリで）: `go build ./...`
Expected: エラーなく成功（このパッケージはまだどこからも参照されないため既存コードへの影響なし）

- [ ] **Step 4: コミット**

```bash
git add internal/domain/user/entity.go internal/domain/user/repository.go
git commit -m "feat: add user domain entity and repository interface"
```

---

### Task 2: PgUserRepository（PostgreSQL実装）

**Files:**
- Create: `go-api/internal/infrastructure/persistence/user_repository.go`
- Test: `go-api/internal/infrastructure/persistence/user_repository_test.go`

**Interfaces:**
- Consumes: `user.User`, `user.Repository`, `user.ErrEmailAlreadyExists`, `user.ErrNotFound`（Task 1）
- Produces:
  - `func persistence.NewPgUserRepository(conn *pgx.Conn) *PgUserRepository`
  - `func (r *PgUserRepository) Create(ctx context.Context, u user.User) error`
  - `func (r *PgUserRepository) FindByEmail(ctx context.Context, email string) (user.User, error)`

- [ ] **Step 1: 失敗するテストを書く**

```go
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
	return ctx, persistence.NewPgUserRepository(conn), func() { conn.Close(ctx) }
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
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgUserRepository -v`
Expected: FAIL（`persistence.NewPgUserRepository`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package persistence

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
)

const uniqueViolationCode = "23505"

type PgUserRepository struct {
	conn *pgx.Conn
}

func NewPgUserRepository(conn *pgx.Conn) *PgUserRepository {
	return &PgUserRepository{conn: conn}
}

func (r *PgUserRepository) Create(ctx context.Context, u user.User) error {
	_, err := r.conn.Exec(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)`,
		u.Email, u.PasswordHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return user.ErrEmailAlreadyExists
		}
		return err
	}
	return nil
}

func (r *PgUserRepository) FindByEmail(ctx context.Context, email string) (user.User, error) {
	var u user.User
	err := r.conn.QueryRow(ctx,
		`SELECT id, email, password_hash, slack_webhook_url, created_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.SlackWebhookURL, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, user.ErrNotFound
		}
		return user.User{}, err
	}
	return u, nil
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgUserRepository -v`
Expected: PASS（PostgreSQLが起動していない場合は`DATABASE_URL not set`または接続エラーでスキップ／失敗するため、事前にローカルDBを用意すること）

- [ ] **Step 5: コミット**

```bash
git add internal/infrastructure/persistence/user_repository.go internal/infrastructure/persistence/user_repository_test.go
git commit -m "feat: add PgUserRepository with Create and FindByEmail"
```

---

### Task 3: domain/auth インターフェース（PasswordHasher・TokenService）

**Files:**
- Create: `go-api/internal/domain/auth/password_hasher.go`
- Create: `go-api/internal/domain/auth/token_service.go`

**Interfaces:**
- Produces:
  - `type auth.PasswordHasher interface { Hash(password string) (string, error); Verify(hash, password string) error }`
  - `type auth.TokenService interface { IssueToken(userID string) (string, error); VerifyToken(token string) (userID string, err error) }`
  - `var auth.ErrInvalidToken = errors.New("invalid token")`

- [ ] **Step 1: password_hasher.goを作成**

```go
package auth

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) error
}
```

- [ ] **Step 2: token_service.goを作成**

```go
package auth

import "errors"

var ErrInvalidToken = errors.New("invalid token")

type TokenService interface {
	IssueToken(userID string) (string, error)
	VerifyToken(token string) (userID string, err error)
}
```

- [ ] **Step 3: ビルド確認**

Run: `go build ./...`
Expected: エラーなく成功

- [ ] **Step 4: コミット**

```bash
git add internal/domain/auth/password_hasher.go internal/domain/auth/token_service.go
git commit -m "feat: add auth domain interfaces (PasswordHasher, TokenService)"
```

---

### Task 4: BcryptHasher実装

**Files:**
- Create: `go-api/internal/interface/gateway/bcrypt_hasher.go`
- Test: `go-api/internal/interface/gateway/bcrypt_hasher_test.go`

**Interfaces:**
- Consumes: `auth.PasswordHasher`（Task 3、型としての整合性確認のみ。パッケージのimportは不要）
- Produces:
  - `func gateway.NewBcryptHasher() *BcryptHasher`
  - `func (h *BcryptHasher) Hash(password string) (string, error)`
  - `func (h *BcryptHasher) Verify(hash, password string) error`

- [ ] **Step 1: 失敗するテストを書く**

```go
package gateway_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBcryptHasher_HashAndVerify(t *testing.T) {
	h := gateway.NewBcryptHasher()

	hash, err := h.Hash("correct-password")
	require.NoError(t, err)
	assert.NotEqual(t, "correct-password", hash)

	assert.NoError(t, h.Verify(hash, "correct-password"))
	assert.Error(t, h.Verify(hash, "wrong-password"))
}
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/interface/gateway/... -run TestBcryptHasher -v`
Expected: FAIL（`gateway.NewBcryptHasher`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package gateway

import "golang.org/x/crypto/bcrypt"

type BcryptHasher struct{}

func NewBcryptHasher() *BcryptHasher {
	return &BcryptHasher{}
}

func (h *BcryptHasher) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (h *BcryptHasher) Verify(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
```

- [ ] **Step 4: go.modを直接依存に更新**

Run: `go get golang.org/x/crypto@v0.40.0`
Expected: `go.mod`の`golang.org/x/crypto`が`// indirect`表記から外れる（既にv0.40.0がgo.sumにあるためバージョンは変わらない）

- [ ] **Step 5: テストを実行して成功を確認**

Run: `go test -race ./internal/interface/gateway/... -run TestBcryptHasher -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add internal/interface/gateway/bcrypt_hasher.go internal/interface/gateway/bcrypt_hasher_test.go go.mod go.sum
git commit -m "feat: add BcryptHasher implementing auth.PasswordHasher"
```

---

### Task 5: JWTTokenService実装

**Files:**
- Create: `go-api/internal/interface/gateway/jwt_token_service.go`
- Test: `go-api/internal/interface/gateway/jwt_token_service_test.go`

**Interfaces:**
- Consumes: `auth.ErrInvalidToken`（Task 3）
- Produces:
  - `func gateway.NewJWTTokenService(secret string) *JWTTokenService`
  - `func (s *JWTTokenService) IssueToken(userID string) (string, error)`
  - `func (s *JWTTokenService) VerifyToken(tokenString string) (string, error)`

- [ ] **Step 1: 依存パッケージを追加**

Run: `go get github.com/golang-jwt/jwt/v5`
Expected: `go.mod`に`github.com/golang-jwt/jwt/v5`が追加される

- [ ] **Step 2: 失敗するテストを書く**

```go
package gateway_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTTokenService_IssueAndVerify(t *testing.T) {
	svc := gateway.NewJWTTokenService("test-secret")

	token, err := svc.IssueToken("user-123")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	userID, err := svc.VerifyToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", userID)
}

func TestJWTTokenService_VerifyToken_WrongSecret(t *testing.T) {
	issuer := gateway.NewJWTTokenService("secret-a")
	verifier := gateway.NewJWTTokenService("secret-b")

	token, err := issuer.IssueToken("user-123")
	require.NoError(t, err)

	_, err = verifier.VerifyToken(token)
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWTTokenService_VerifyToken_Expired(t *testing.T) {
	svc := gateway.NewJWTTokenService("test-secret")
	claims := jwt.RegisteredClaims{
		Subject:   "user-123",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
	}
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := expiredToken.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, err = svc.VerifyToken(signed)
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `go test -race ./internal/interface/gateway/... -run TestJWTTokenService -v`
Expected: FAIL（`gateway.NewJWTTokenService`が未定義でコンパイルエラー）

- [ ] **Step 4: 実装を書く**

```go
package gateway

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
)

const tokenExpiry = 24 * time.Hour

type JWTTokenService struct {
	secret []byte
}

func NewJWTTokenService(secret string) *JWTTokenService {
	return &JWTTokenService{secret: []byte(secret)}
}

func (s *JWTTokenService) IssueToken(userID string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExpiry)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *JWTTokenService) VerifyToken(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return "", auth.ErrInvalidToken
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.Subject == "" {
		return "", auth.ErrInvalidToken
	}
	return claims.Subject, nil
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `go test -race ./internal/interface/gateway/... -run TestJWTTokenService -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add internal/interface/gateway/jwt_token_service.go internal/interface/gateway/jwt_token_service_test.go go.mod go.sum
git commit -m "feat: add JWTTokenService implementing auth.TokenService"
```

---

### Task 6: watchlist domain拡張＋PgWatchlistRepository CRUD実装

**Files:**
- Modify: `go-api/internal/domain/watchlist/repository.go`
- Modify: `go-api/internal/infrastructure/persistence/watchlist_repository.go`
- Modify: `go-api/internal/infrastructure/persistence/watchlist_repository_test.go`

**Interfaces:**
- Consumes: `watchlist.Watchlist`（既存）, `stock.NewStockCode`（既存）
- Produces:
  - `type watchlist.Repository interface` に `Create(ctx, w Watchlist) (Watchlist, error)`、`Delete(ctx, id, userID string) error`、`UpdateThreshold(ctx, id, userID string, threshold float64) error` を追加
  - `var watchlist.ErrAlreadyExists = errors.New("stock already in watchlist")`
  - `var watchlist.ErrNotFound = errors.New("watchlist item not found")`
  - `func (r *PgWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error)`
  - `func (r *PgWatchlistRepository) Delete(ctx context.Context, id, userID string) error`
  - `func (r *PgWatchlistRepository) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error`

- [ ] **Step 1: repository.goのインターフェースを拡張**

`go-api/internal/domain/watchlist/repository.go`を以下の内容で置き換える:

```go
package watchlist

import (
	"context"
	"errors"
)

var ErrAlreadyExists = errors.New("stock already in watchlist")
var ErrNotFound = errors.New("watchlist item not found")

type Repository interface {
	FindByUserID(ctx context.Context, userID string) ([]Watchlist, error)
	Create(ctx context.Context, w Watchlist) (Watchlist, error)
	Delete(ctx context.Context, id, userID string) error
	UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error
}
```

- [ ] **Step 2: 失敗するテストを既存テストファイルに追記**

`go-api/internal/infrastructure/persistence/watchlist_repository_test.go`の末尾（既存の`TestPgWatchlistRepository_FindByUserID_Empty`の後）に追記:

```go

func insertTestUser(t *testing.T, ctx context.Context, conn *pgx.Conn, email string) string {
	t.Helper()
	var id string
	err := conn.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'hashed') RETURNING id",
		email,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestPgWatchlistRepository_CreateAndFindByUserID(t *testing.T) {
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
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	userID := insertTestUser(t, ctx, conn, "watchlist-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 3.0})
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, code, found[0].StockCode)
	assert.Equal(t, 3.0, found[0].AlertThreshold)
}

func TestPgWatchlistRepository_Create_Duplicate(t *testing.T) {
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
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	userID := insertTestUser(t, ctx, conn, "dup-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)
	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.ErrorIs(t, err, watchlist.ErrAlreadyExists)
}

func TestPgWatchlistRepository_Delete(t *testing.T) {
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
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	ownerID := insertTestUser(t, ctx, conn, "delete-owner@example.com")
	otherID := insertTestUser(t, ctx, conn, "delete-other@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: ownerID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)

	err = repo.Delete(ctx, created.ID, otherID)
	assert.ErrorIs(t, err, watchlist.ErrNotFound)

	err = repo.Delete(ctx, created.ID, ownerID)
	require.NoError(t, err)

	found, err := repo.FindByUserID(ctx, ownerID)
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestPgWatchlistRepository_UpdateThreshold(t *testing.T) {
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
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	userID := insertTestUser(t, ctx, conn, "update-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)

	err = repo.UpdateThreshold(ctx, created.ID, userID, 4.0)
	require.NoError(t, err)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, 4.0, found[0].AlertThreshold)
}
```

このテストファイルの先頭importに`"github.com/jackc/pgx/v5"`と`"github.com/stock-anomaly-detection/go-api/internal/domain/stock"`を追加する。

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgWatchlistRepository -v`
Expected: FAIL（`Create`/`Delete`/`UpdateThreshold`メソッドが未定義でコンパイルエラー）

- [ ] **Step 4: PgWatchlistRepositoryにCRUDメソッドを実装**

`go-api/internal/infrastructure/persistence/watchlist_repository.go`の末尾に追記（importに`"errors"`と`"github.com/jackc/pgx/v5/pgconn"`を追加）:

```go

func (r *PgWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	err := r.conn.QueryRow(ctx,
		`INSERT INTO watchlist (user_id, stock_code, alert_threshold) VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		w.UserID, w.StockCode.String(), w.AlertThreshold,
	).Scan(&w.ID, &w.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return watchlist.Watchlist{}, watchlist.ErrAlreadyExists
		}
		return watchlist.Watchlist{}, err
	}
	return w, nil
}

func (r *PgWatchlistRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.conn.Exec(ctx,
		`DELETE FROM watchlist WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watchlist.ErrNotFound
	}
	return nil
}

func (r *PgWatchlistRepository) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	tag, err := r.conn.Exec(ctx,
		`UPDATE watchlist SET alert_threshold = $1 WHERE id = $2 AND user_id = $3`,
		threshold, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watchlist.ErrNotFound
	}
	return nil
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/... -run TestPgWatchlistRepository -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add internal/domain/watchlist/repository.go internal/infrastructure/persistence/watchlist_repository.go internal/infrastructure/persistence/watchlist_repository_test.go
git commit -m "feat: add Create, Delete, UpdateThreshold to watchlist repository"
```

---

### Task 7: RegisterUserUsecase

**Files:**
- Create: `go-api/internal/usecase/register_user.go`
- Test: `go-api/internal/usecase/register_user_test.go`

**Interfaces:**
- Consumes: `user.User`, `user.Repository`, `user.ErrEmailAlreadyExists`（Task 1）, `auth.PasswordHasher`（Task 3）
- Produces:
  - `var usecase.ErrInvalidEmail = errors.New("invalid email format")`
  - `var usecase.ErrPasswordTooShort = errors.New("password must be at least 8 characters")`
  - `func usecase.NewRegisterUserUsecase(users user.Repository, hasher auth.PasswordHasher) *RegisterUserUsecase`
  - `func (u *RegisterUserUsecase) Handle(ctx context.Context, email, password string) error`

- [ ] **Step 1: 失敗するテストを書く**

```go
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
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/usecase/... -run TestRegisterUserUsecase -v`
Expected: FAIL（`usecase.NewRegisterUserUsecase`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package usecase

import (
	"context"
	"errors"
	"net/mail"

	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
)

var ErrInvalidEmail = errors.New("invalid email format")
var ErrPasswordTooShort = errors.New("password must be at least 8 characters")

const minPasswordLength = 8

type RegisterUserUsecase struct {
	users  user.Repository
	hasher auth.PasswordHasher
}

func NewRegisterUserUsecase(users user.Repository, hasher auth.PasswordHasher) *RegisterUserUsecase {
	return &RegisterUserUsecase{users: users, hasher: hasher}
}

func (u *RegisterUserUsecase) Handle(ctx context.Context, email, password string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	if len(password) < minPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := u.hasher.Hash(password)
	if err != nil {
		return err
	}
	return u.users.Create(ctx, user.User{Email: email, PasswordHash: hash})
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `go test -race ./internal/usecase/... -run TestRegisterUserUsecase -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add internal/usecase/register_user.go internal/usecase/register_user_test.go
git commit -m "feat: add RegisterUserUsecase"
```

---

### Task 8: LoginUserUsecase

**Files:**
- Create: `go-api/internal/usecase/login_user.go`
- Test: `go-api/internal/usecase/login_user_test.go`

**Interfaces:**
- Consumes: `user.User`, `user.Repository`, `user.ErrNotFound`（Task 1）, `auth.PasswordHasher`, `auth.TokenService`（Task 3）
- Produces:
  - `var usecase.ErrInvalidCredentials = errors.New("invalid email or password")`
  - `func usecase.NewLoginUserUsecase(users user.Repository, hasher auth.PasswordHasher, tokens auth.TokenService) *LoginUserUsecase`
  - `func (u *LoginUserUsecase) Handle(ctx context.Context, email, password string) (string, error)`

- [ ] **Step 1: 失敗するテストを書く**

```go
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
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/usecase/... -run TestLoginUserUsecase -v`
Expected: FAIL（`usecase.NewLoginUserUsecase`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
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
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `go test -race ./internal/usecase/... -run TestLoginUserUsecase -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add internal/usecase/login_user.go internal/usecase/login_user_test.go
git commit -m "feat: add LoginUserUsecase"
```

---

### Task 9: ManageWatchlistUsecase

**Files:**
- Create: `go-api/internal/usecase/manage_watchlist.go`
- Test: `go-api/internal/usecase/manage_watchlist_test.go`

**Interfaces:**
- Consumes: `watchlist.Watchlist`, `watchlist.Repository`（Task 6, 既存の`FindByUserID`含む）, `stock.NewStockCode`（既存）
- Produces:
  - `func usecase.NewManageWatchlistUsecase(watchlists watchlist.Repository) *ManageWatchlistUsecase`
  - `func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error)`
  - `func (u *ManageWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error`
  - `func (u *ManageWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error`
  - `func (u *ManageWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error)`

- [ ] **Step 1: 失敗するテストを書く**

```go
package usecase_test

import (
	"context"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockWatchlistRepository struct{ mock.Mock }

func (m *mockWatchlistRepository) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]watchlist.Watchlist), args.Error(1)
}
func (m *mockWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	args := m.Called(ctx, w)
	return args.Get(0).(watchlist.Watchlist), args.Error(1)
}
func (m *mockWatchlistRepository) Delete(ctx context.Context, id, userID string) error {
	return m.Called(ctx, id, userID).Error(0)
}
func (m *mockWatchlistRepository) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	return m.Called(ctx, id, userID, threshold).Error(0)
}

func TestManageWatchlistUsecase_Add_Success(t *testing.T) {
	repo := new(mockWatchlistRepository)
	code, _ := stock.NewStockCode("7203")
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}

func TestManageWatchlistUsecase_Add_DefaultThreshold(t *testing.T) {
	repo := new(mockWatchlistRepository)
	code, _ := stock.NewStockCode("7203")
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 2.5}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 2.5}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo)
	_, err := uc.Add(context.Background(), "user-1", "7203", 0)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_InvalidStockCode(t *testing.T) {
	repo := new(mockWatchlistRepository)

	uc := usecase.NewManageWatchlistUsecase(repo)
	_, err := uc.Add(context.Background(), "user-1", "invalid", 2.5)

	require.Error(t, err)
	repo.AssertNotCalled(t, "Create")
}

func TestManageWatchlistUsecase_Remove(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("Delete", mock.Anything, "wl-1", "user-1").Return(nil)

	uc := usecase.NewManageWatchlistUsecase(repo)
	err := uc.Remove(context.Background(), "user-1", "wl-1")

	require.NoError(t, err)
}

func TestManageWatchlistUsecase_UpdateThreshold(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("UpdateThreshold", mock.Anything, "wl-1", "user-1", 4.0).Return(nil)

	uc := usecase.NewManageWatchlistUsecase(repo)
	err := uc.UpdateThreshold(context.Background(), "user-1", "wl-1", 4.0)

	require.NoError(t, err)
}

func TestManageWatchlistUsecase_List(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{ID: "wl-1"}}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo)
	result, err := uc.List(context.Background(), "user-1")

	require.NoError(t, err)
	require.Len(t, result, 1)
}
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: FAIL（`usecase.NewManageWatchlistUsecase`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package usecase

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const defaultAlertThreshold = 2.5

type ManageWatchlistUsecase struct {
	watchlists watchlist.Repository
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists}
}

func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}
	return u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		AlertThreshold: threshold,
	})
}

func (u *ManageWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return u.watchlists.Delete(ctx, watchlistID, userID)
}

func (u *ManageWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	return u.watchlists.UpdateThreshold(ctx, watchlistID, userID, threshold)
}

func (u *ManageWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return u.watchlists.FindByUserID(ctx, userID)
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `go test -race ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add internal/usecase/manage_watchlist.go internal/usecase/manage_watchlist_test.go
git commit -m "feat: add ManageWatchlistUsecase"
```

---

### Task 10: レスポンスヘルパー・auth_handler.go

**Files:**
- Create: `go-api/internal/interface/handler/response.go`
- Create: `go-api/internal/interface/handler/auth_handler.go`
- Test: `go-api/internal/interface/handler/auth_handler_test.go`

**Interfaces:**
- Consumes: `user.ErrEmailAlreadyExists`（Task 1）, `usecase.ErrInvalidEmail`, `usecase.ErrPasswordTooShort`（Task 7）, `usecase.ErrInvalidCredentials`（Task 8）
- Produces:
  - `func handler.writeJSON(w http.ResponseWriter, status int, v interface{})`（パッケージ内限定）
  - `func handler.writeError(w http.ResponseWriter, status int, message string)`（パッケージ内限定、Task 12で使用）
  - `func handler.NewAuthHandler(register registerUsecase, login loginUsecase) *AuthHandler`
  - `func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request)`
  - `func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: response.goを作成**

```go
package handler

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
```

- [ ] **Step 2: 失敗するテストを書く**

```go
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRegisterUsecase struct {
	err error
}

func (s stubRegisterUsecase) Handle(ctx context.Context, email, password string) error {
	return s.err
}

type stubLoginUsecase struct {
	token string
	err   error
}

func (s stubLoginUsecase) Handle(ctx context.Context, email, password string) (string, error) {
	return s.token, s.err
}

func TestAuthHandler_Register_Success(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: nil}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: user.ErrEmailAlreadyExists}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestAuthHandler_Register_InvalidEmail(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: usecase.ErrInvalidEmail}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "bad", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_Login_Success(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{}, stubLoginUsecase{token: "signed-token", err: nil})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "signed-token", resp["token"])
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{}, stubLoginUsecase{err: usecase.ErrInvalidCredentials})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `go test -race ./internal/interface/handler/... -run TestAuthHandler -v`
Expected: FAIL（`handler.NewAuthHandler`が未定義でコンパイルエラー）

- [ ] **Step 4: auth_handler.goを実装**

```go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

type registerUsecase interface {
	Handle(ctx context.Context, email, password string) error
}

type loginUsecase interface {
	Handle(ctx context.Context, email, password string) (string, error)
}

type AuthHandler struct {
	register registerUsecase
	login    loginUsecase
}

func NewAuthHandler(register registerUsecase, login loginUsecase) *AuthHandler {
	return &AuthHandler{register: register, login: login}
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := h.register.Handle(r.Context(), req.Email, req.Password)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusCreated)
	case errors.Is(err, user.ErrEmailAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, usecase.ErrInvalidEmail), errors.Is(err, usecase.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, err := h.login.Handle(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, usecase.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `go test -race ./internal/interface/handler/... -run TestAuthHandler -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add internal/interface/handler/response.go internal/interface/handler/auth_handler.go internal/interface/handler/auth_handler_test.go
git commit -m "feat: add AuthHandler for register and login endpoints"
```

---

### Task 11: auth_middleware.go

**Files:**
- Create: `go-api/internal/interface/handler/auth_middleware.go`
- Test: `go-api/internal/interface/handler/auth_middleware_test.go`

**Interfaces:**
- Consumes: `writeError`（Task 10、パッケージ内）
- Produces:
  - `func handler.RequireAuth(tokens tokenVerifier, next http.HandlerFunc) http.HandlerFunc`
  - `func handler.userIDFromContext(ctx context.Context) (string, bool)`（パッケージ内限定、Task 12で使用）

- [ ] **Step 1: 失敗するテストを書く**

```go
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stretchr/testify/assert"
)

type stubTokenVerifier struct {
	userID string
	err    error
}

func (s stubTokenVerifier) VerifyToken(token string) (string, error) {
	return s.userID, s.err
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := handler.RequireAuth(stubTokenVerifier{}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := handler.RequireAuth(stubTokenVerifier{err: assert.AnError}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_Success(t *testing.T) {
	called := false
	next := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}
	mw := handler.RequireAuth(stubTokenVerifier{userID: "user-1"}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/interface/handler/... -run TestRequireAuth -v`
Expected: FAIL（`handler.RequireAuth`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package handler

import (
	"context"
	"net/http"
	"strings"
)

type tokenVerifier interface {
	VerifyToken(token string) (userID string, err error)
}

type contextKey string

const userIDContextKey contextKey = "user_id"

func RequireAuth(tokens tokenVerifier, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, prefix)
		userID, err := tokens.VerifyToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), userIDContextKey, userID)
		next(w, r.WithContext(ctx))
	}
}

func userIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `go test -race ./internal/interface/handler/... -run TestRequireAuth -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add internal/interface/handler/auth_middleware.go internal/interface/handler/auth_middleware_test.go
git commit -m "feat: add RequireAuth JWT middleware"
```

---

### Task 12: watchlist_handler.go

**Files:**
- Create: `go-api/internal/interface/handler/watchlist_handler.go`
- Test: `go-api/internal/interface/handler/watchlist_handler_test.go`

**Interfaces:**
- Consumes: `watchlist.Watchlist`, `watchlist.ErrAlreadyExists`, `watchlist.ErrNotFound`（Task 6）, `writeJSON`/`writeError`（Task 10）, `userIDFromContext`/`userIDContextKey`（Task 11）
- Produces:
  - `func handler.NewWatchlistHandler(watchlists watchlistUsecase) *WatchlistHandler`
  - `func (h *WatchlistHandler) List(w http.ResponseWriter, r *http.Request)`
  - `func (h *WatchlistHandler) Add(w http.ResponseWriter, r *http.Request)`
  - `func (h *WatchlistHandler) Remove(w http.ResponseWriter, r *http.Request)`
  - `func (h *WatchlistHandler) UpdateThreshold(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: 失敗するテストを書く**

```go
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubWatchlistUsecase struct {
	addResult watchlist.Watchlist
	addErr    error
	removeErr error
	updateErr error
	listResult []watchlist.Watchlist
	listErr   error
}

func (s stubWatchlistUsecase) Add(ctx context.Context, userID, stockCode string, threshold float64) (watchlist.Watchlist, error) {
	return s.addResult, s.addErr
}
func (s stubWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return s.removeErr
}
func (s stubWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	return s.updateErr
}
func (s stubWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return s.listResult, s.listErr
}

func withUserContext(req *http.Request, userID string) *http.Request {
	verifier := stubTokenVerifier{userID: userID}
	var captured *http.Request
	mw := handler.RequireAuth(verifier, func(w http.ResponseWriter, r *http.Request) { captured = r })
	req.Header.Set("Authorization", "Bearer any-token")
	mw(httptest.NewRecorder(), req)
	return captured
}

func TestWatchlistHandler_List(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{listResult: []watchlist.Watchlist{{ID: "wl-1", StockCode: code, AlertThreshold: 2.5}}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/watchlist", nil), "user-1")
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp []map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp, 1)
	assert.Equal(t, "7203", resp[0]["stock_code"])
}

func TestWatchlistHandler_Add_Success(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{addResult: watchlist.Watchlist{ID: "wl-1", StockCode: code, AlertThreshold: 2.5}})
	body, _ := json.Marshal(map[string]interface{}{"stock_code": "7203", "alert_threshold": 2.5})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body)), "user-1")
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestWatchlistHandler_Add_AlreadyExists(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{addErr: watchlist.ErrAlreadyExists})
	body, _ := json.Marshal(map[string]interface{}{"stock_code": "7203", "alert_threshold": 2.5})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body)), "user-1")
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestWatchlistHandler_Remove_NotFound(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{removeErr: watchlist.ErrNotFound})
	req := withUserContext(httptest.NewRequest(http.MethodDelete, "/watchlist/wl-1", nil), "user-1")
	req.SetPathValue("id", "wl-1")
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWatchlistHandler_UpdateThreshold_Success(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{})
	body, _ := json.Marshal(map[string]interface{}{"alert_threshold": 4.0})
	req := withUserContext(httptest.NewRequest(http.MethodPatch, "/watchlist/wl-1", bytes.NewReader(body)), "user-1")
	req.SetPathValue("id", "wl-1")
	rec := httptest.NewRecorder()

	h.UpdateThreshold(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `go test -race ./internal/interface/handler/... -run TestWatchlistHandler -v`
Expected: FAIL（`handler.NewWatchlistHandler`が未定義でコンパイルエラー）

- [ ] **Step 3: 実装を書く**

```go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

type watchlistUsecase interface {
	Add(ctx context.Context, userID, stockCode string, threshold float64) (watchlist.Watchlist, error)
	Remove(ctx context.Context, userID, watchlistID string) error
	UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error
	List(ctx context.Context, userID string) ([]watchlist.Watchlist, error)
}

type WatchlistHandler struct {
	watchlists watchlistUsecase
}

func NewWatchlistHandler(watchlists watchlistUsecase) *WatchlistHandler {
	return &WatchlistHandler{watchlists: watchlists}
}

type watchlistItemResponse struct {
	ID             string  `json:"id"`
	StockCode      string  `json:"stock_code"`
	AlertThreshold float64 `json:"alert_threshold"`
}

func toWatchlistItemResponse(w watchlist.Watchlist) watchlistItemResponse {
	return watchlistItemResponse{ID: w.ID, StockCode: w.StockCode.String(), AlertThreshold: w.AlertThreshold}
}

func (h *WatchlistHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	items, err := h.watchlists.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	resp := make([]watchlistItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toWatchlistItemResponse(item))
	}
	writeJSON(w, http.StatusOK, resp)
}

type addWatchlistRequest struct {
	StockCode      string  `json:"stock_code"`
	AlertThreshold float64 `json:"alert_threshold"`
}

func (h *WatchlistHandler) Add(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	var req addWatchlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.watchlists.Add(r.Context(), userID, req.StockCode, req.AlertThreshold)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, toWatchlistItemResponse(item))
	case errors.Is(err, watchlist.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (h *WatchlistHandler) Remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	id := r.PathValue("id")
	err := h.watchlists.Remove(r.Context(), userID, id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

type updateThresholdRequest struct {
	AlertThreshold float64 `json:"alert_threshold"`
}

func (h *WatchlistHandler) UpdateThreshold(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	id := r.PathValue("id")
	var req updateThresholdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := h.watchlists.UpdateThreshold(r.Context(), userID, id, req.AlertThreshold)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `go test -race ./internal/interface/handler/... -run TestWatchlistHandler -v`
Expected: PASS

- [ ] **Step 5: パッケージ全体のテストを実行**

Run: `go test -race ./internal/interface/handler/... -v`
Expected: PASS（Task 10〜12の全テストが通る）

- [ ] **Step 6: コミット**

```bash
git add internal/interface/handler/watchlist_handler.go internal/interface/handler/watchlist_handler_test.go
git commit -m "feat: add WatchlistHandler for CRUD endpoints"
```

---

### Task 13: main.go配線・環境変数ドキュメント更新

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: 全タスクのコンストラクタ関数（`NewPgUserRepository`, `NewBcryptHasher`, `NewJWTTokenService`, `NewPgWatchlistRepository`, `NewRegisterUserUsecase`, `NewLoginUserUsecase`, `NewManageWatchlistUsecase`, `NewAuthHandler`, `RequireAuth`, `NewWatchlistHandler`）

- [ ] **Step 1: main.goにHTTPサーバー配線を追加**

`go-api/cmd/api/main.go`のimportブロックに`"net/http"`と`"github.com/stock-anomaly-detection/go-api/internal/interface/handler"`を追加。

`jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")`の下あたりに新規環境変数の読み込みを追加:

```go
	jwtSecret := mustEnv("JWT_SECRET")

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
```

`notificationRepo := persistence.NewPgNotificationRepository(conn)`の下、`notifyUsecase := ...`の前後に以下を追加:

```go
	userRepo := persistence.NewPgUserRepository(conn)
	watchlistRepo := persistence.NewPgWatchlistRepository(conn)
	hasher := gateway.NewBcryptHasher()
	tokenService := gateway.NewJWTTokenService(jwtSecret)

	registerUsecase := usecase.NewRegisterUserUsecase(userRepo, hasher)
	loginUsecase := usecase.NewLoginUserUsecase(userRepo, hasher, tokenService)
	watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo)

	authHandler := handler.NewAuthHandler(registerUsecase, loginUsecase)
	watchlistHandler := handler.NewWatchlistHandler(watchlistUsecase)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/register", authHandler.Register)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("GET /watchlist", handler.RequireAuth(tokenService, watchlistHandler.List))
	mux.HandleFunc("POST /watchlist", handler.RequireAuth(tokenService, watchlistHandler.Add))
	mux.HandleFunc("DELETE /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.Remove))
	mux.HandleFunc("PATCH /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.UpdateThreshold))

	go func() {
		log.Printf("http server listening on :%s", port)
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			log.Printf("ERROR http server: %v", err)
		}
	}()
```

これを`monitor.StartMonitoring(ctx, codes, pollHour, pollMinute)`呼び出しより前に配置する（既存の監視ループ起動処理自体は変更しない）。

- [ ] **Step 2: ビルド確認**

Run: `go build ./...`
Expected: エラーなく成功

- [ ] **Step 3: 既存テストを含めた全体テストを実行**

Run: `go test -race -short ./...`
Expected: PASS（既存の全テスト＋今回追加した全テストが通る。統合テストは`DATABASE_URL`未設定のためスキップされる）

- [ ] **Step 4: CLAUDE.mdの環境変数一覧を更新**

`CLAUDE.md`の「## 環境変数（本番）」セクションを以下に置き換える:

```
## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）, FINNHUB_API_KEY, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

- [ ] **Step 5: コミット**

```bash
git add cmd/api/main.go
git add ../CLAUDE.md
git commit -m "feat: wire user auth and watchlist API into main.go"
```

---

## Self-Review メモ

- **spec カバレッジ**: 設計書の全セクション（domain/usecase/interface/infrastructure/main.go配線/テスト方針/エラーハンドリング）がTask 1〜13に対応することを確認済み。
- **プレースホルダー**: 「TBD」「後で実装」等の記述なし。全ステップに実コードあり。
- **型整合性**: `user.Repository`・`watchlist.Repository`・`auth.PasswordHasher`・`auth.TokenService`のシグネチャは全タスクで統一（`Task 1/3/6`で定義した型を`Task 2/4/5/7/8/9`がそのまま利用し、`Task 10/11/12`のハンドラは`context.Context`ベースの最小インターフェースで受け取る）。
