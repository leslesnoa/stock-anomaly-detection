# Phase 5: Docker化・Railway/Vercelデプロイ Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** go-apiにヘルスチェックエンドポイントとDockerfileを追加し、frontendのビルド時エラーを解消して、Railway（go-api・python-engine・PostgreSQL・Redis）とVercel（frontend）に実際にデプロイできる状態にする。

**Architecture:** 既存の3コンポーネント（go-api・python-engine・frontend）はコード変更を最小限に抑える。go-apiにDockerfileと`/health`を追加、frontendのGO_API_URL未設定ガードをビルド時に発火しない遅延評価に変更する。python-engineは既存Dockerfileをそのまま使う。最後にRailway/Vercelへの実際のデプロイ手順をdocsにまとめる（実際のデプロイ操作自体はユーザーのアカウント認証が必要なため、このプランのスコープ外）。

**Tech Stack:** Go 1.24 / net/http / testify、Next.js（Vitest）、Docker（マルチステージビルド、alpine）

**Spec:** `docs/superpowers/specs/2026-09-21-phase5-docker-railway-deploy-design.md`

## Global Constraints

- 実装作業は`feature/phase5-docker-railway-deploy`ブランチ上で行う（mainで直接作業しない、既にチェックアウト済み）
- go-apiの単体テストは `go test -race -short ./...`（`go-api/`ディレクトリから実行、外部サービス不要）
- go-apiのビルド確認は `go build ./...`（`go-api/`ディレクトリから実行）
- frontendのテストは `npm test`（`frontend/`ディレクトリから実行、内部的に`vitest run`）
- frontendのビルド確認は `npm run build`（`frontend/`ディレクトリから実行、内部的に`next build`）
- ステージング環境は作らない。本番環境のみのシンプルな構成
- DBマイグレーション自動化ツール（golang-migrate等）は導入しない
- GitHub Actions経由のデプロイ制御は行わない。Railway/VercelのネイティブGit連携に任せる
- Dockerfileはリポジトリルートをビルドコンテキストとする前提で書く（既存`python-engine/Dockerfile`と同じパターン）

---

## Task 1: `GET /health` エンドポイント追加

**Files:**
- Create: `go-api/internal/interface/handler/health_handler.go`
- Test: `go-api/internal/interface/handler/health_handler_test.go`
- Modify: `go-api/cmd/api/main.go`（`mux.HandleFunc`登録の追加のみ）

**Interfaces:**
- Consumes: `writeJSON(w http.ResponseWriter, status int, v interface{})`（`go-api/internal/interface/handler/response.go`に既存）
- Produces: `handler.Health(w http.ResponseWriter, r *http.Request)` — Task 2でDockerコンテナの起動確認に使うHTTPハンドラ

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/interface/handler/health_handler_test.go`を新規作成:

```go
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stretchr/testify/assert"
)

func TestHealth_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run（`go-api/`ディレクトリから）: `go test ./internal/interface/handler/... -run TestHealth -v`
Expected: FAIL（`handler.Health`が未定義でコンパイルエラー）

- [ ] **Step 3: 最小限の実装を書く**

`go-api/internal/interface/handler/health_handler.go`を新規作成:

```go
package handler

import "net/http"

func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: テストを実行してパスを確認する**

Run（`go-api/`ディレクトリから）: `go test ./internal/interface/handler/... -run TestHealth -v`
Expected: PASS

- [ ] **Step 5: `main.go`にルートを登録する**

`go-api/cmd/api/main.go`の`mux.HandleFunc("POST /auth/register", ...)`の直前に1行追加:

```go
	mux.HandleFunc("GET /health", handler.Health)
	mux.HandleFunc("POST /auth/register", authHandler.Register)
```

（認証不要のエンドポイントなので`handler.RequireAuth`でラップしない）

- [ ] **Step 6: 全体テストとビルドを確認する**

Run（`go-api/`ディレクトリから）: `go test -race -short ./...` → 全PASS
Run（`go-api/`ディレクトリから）: `go build ./...` → エラーなし

- [ ] **Step 7: コミット**

```bash
git add go-api/internal/interface/handler/health_handler.go go-api/internal/interface/handler/health_handler_test.go go-api/cmd/api/main.go
git commit -m "feat: add GET /health endpoint for Railway healthcheck"
```

---

## Task 2: `go-api/Dockerfile` 新規作成とローカル動作確認

**Files:**
- Create: `go-api/Dockerfile`

**Interfaces:**
- Consumes: Task 1で追加した`GET /health`エンドポイント（起動確認に使う）
- Consumes: `go-api/cmd/api/main.go`の起動時必須環境変数（`REDIS_URL`・`ANTHROPIC_API_KEY`・`SLACK_WEBHOOK_URL`・`PYTHON_ENGINE_URL`・`JWT_SECRET`・`DATABASE_URL`。未設定だと`mustEnv`が`log.Fatalf`でプロセスを終了する）
- Produces: このDockerfileがRailwayのgo-apiサービスのビルド定義になる（Task 4のセットアップ手順書から参照される）

- [ ] **Step 1: Dockerfileを作成する**

`go-api/Dockerfile`を新規作成（`python-engine/Dockerfile`と同じく、リポジトリルートをビルドコンテキストとする前提）:

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /src

# 依存解決（ソースコードより先にコピーしてレイヤーキャッシュを活かす）
COPY go-api/go.mod go-api/go.sum ./
RUN go mod download

# アプリケーションコードをコピーしてビルド
COPY go-api/ ./
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api

FROM alpine:3.20

# Yahoo Finance / Claude API / Slack / yanoshin.jp への外部HTTPS通信にCA証明書が必要
RUN apk add --no-cache ca-certificates

COPY --from=builder /bin/api /bin/api

EXPOSE 8080

CMD ["/bin/api"]
```

- [ ] **Step 2: リポジトリルートからDockerイメージのビルドを確認する**

Run（リポジトリルートから）: `docker build -f go-api/Dockerfile -t stock-anomaly-go-api .`
Expected: `Successfully tagged stock-anomaly-go-api:latest`（ビルドエラーなし）

- [ ] **Step 3: ローカルのpostgres/redisを起動する**

Run（リポジトリルートから）: `docker compose up -d postgres redis`
Expected: 両コンテナが`healthy`になる（`docker compose ps`で確認。`docker-compose.yml`は`go-api/migrations`を`/docker-entrypoint-initdb.d`にマウントしているため、postgres初回起動時に`001_initial_schema.sql`が自動適用される）

- [ ] **Step 4: ビルドしたイメージをダミー環境変数で起動する**

Run（リポジトリルートから、`host.docker.internal`はDocker Desktop for Macでホストを指す）:

```bash
docker run --rm -d --name go-api-smoke-test \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://postgres:postgres@host.docker.internal:5432/stock_anomaly?sslmode=disable" \
  -e REDIS_URL="redis://host.docker.internal:6379" \
  -e ANTHROPIC_API_KEY="dummy" \
  -e SLACK_WEBHOOK_URL="https://hooks.slack.com/services/dummy" \
  -e PYTHON_ENGINE_URL="http://localhost:9999" \
  -e JWT_SECRET="dummy-secret" \
  stock-anomaly-go-api
```

Expected: コンテナが起動したまま終了しない（`docker ps`で`go-api-smoke-test`がUP状態）

- [ ] **Step 5: ヘルスチェックを確認する**

Run: `curl -i http://localhost:8080/health`
Expected: `HTTP/1.1 200 OK` と `{"status":"ok"}`

- [ ] **Step 6: 後片付け**

Run:
```bash
docker stop go-api-smoke-test
docker compose down
```

- [ ] **Step 7: コミット**

```bash
git add go-api/Dockerfile
git commit -m "feat: add Dockerfile for go-api"
```

---

## Task 3: frontend `GO_API_URL` ビルド時ガードの遅延評価化

**Files:**
- Modify: `frontend/lib/go-api-client.ts`
- Modify: `frontend/lib/go-api-client.test.ts`

**Interfaces:**
- Consumes: なし（既存ファイルの内部実装のみ変更）
- Produces: `registerUser`・`loginUser`・`fetchWatchlist`・`addWatchlistItem`・`removeWatchlistItem`の関数シグネチャは変更しない（既存の呼び出し元に影響しない）

- [ ] **Step 1: 失敗するテストを書く**

`frontend/lib/go-api-client.test.ts`の末尾（既存の`describe("go-api-client", ...)`ブロックの外、ファイル末尾）に追記:

```ts
describe("go-api-client production guard", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("does not throw when the module is loaded in production without GO_API_URL", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("GO_API_URL", "");

    await expect(import("./go-api-client")).resolves.toBeDefined();
  });

  it("throws when an API call is made in production without GO_API_URL", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("GO_API_URL", "");
    vi.stubGlobal("fetch", vi.fn());

    const { registerUser } = await import("./go-api-client");

    await expect(
      registerUser("a@example.com", "password123"),
    ).rejects.toThrow("GO_API_URL must be set in production");
  });
});
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run（`frontend/`ディレクトリから）: `npm test -- go-api-client.test.ts`
Expected: FAIL（1つ目のテスト`does not throw when the module is loaded...`が、現状のモジュールトップレベルIIFEが即座にthrowするため失敗する）

- [ ] **Step 3: 実装を遅延評価に変更する**

`frontend/lib/go-api-client.ts`の15〜24行目（`const GO_API_URL = (() => {...})();`）を置き換え:

```ts
function getGoApiUrl(): string {
  const url = process.env.GO_API_URL;
  if (!url) {
    if (process.env.NODE_ENV === "production") {
      throw new Error("GO_API_URL must be set in production");
    }
    return "http://localhost:8080";
  }
  return url;
}
```

続けて、同ファイル内の5箇所の`${GO_API_URL}`をすべて`${getGoApiUrl()}`に置き換える（35行目・50行目・65行目・80行目・99行目、`registerUser`・`loginUser`・`fetchWatchlist`・`addWatchlistItem`・`removeWatchlistItem`それぞれの`fetch`呼び出し内）。

- [ ] **Step 4: テストを実行してパスを確認する**

Run（`frontend/`ディレクトリから）: `npm test -- go-api-client.test.ts`
Expected: PASS（新規2件を含む全テスト）

- [ ] **Step 5: フルテストスイートと本番ビルドを確認する**

Run（`frontend/`ディレクトリから）: `npm test` → 全PASS
Run（`frontend/`ディレクトリから）: `GO_API_URL= npm run build` → ビルド成功（`next build`が`NODE_ENV=production`相当で実行されるが、`GO_API_URL`未設定でも失敗しないことを確認する）

- [ ] **Step 6: コミット**

```bash
git add frontend/lib/go-api-client.ts frontend/lib/go-api-client.test.ts
git commit -m "fix: defer GO_API_URL production guard until first API call"
```

---

## Task 4: Railway/Vercelデプロイセットアップ手順書の作成

**Files:**
- Create: `docs/deployment/phase5-railway-vercel-setup.md`

**Interfaces:**
- Consumes: Task 1〜3で追加した`/health`・`go-api/Dockerfile`・frontendの遅延ガード
- Produces: なし（このタスクはドキュメントのみ。実際のデプロイ実行はこの計画のスコープ外で、ユーザーと一緒に本手順書に従って進める）

- [ ] **Step 1: 手順書を作成する**

`docs/deployment/phase5-railway-vercel-setup.md`を新規作成:

```markdown
# Railway / Vercel デプロイセットアップ手順

設計: `docs/superpowers/specs/2026-09-21-phase5-docker-railway-deploy-design.md`

このドキュメントの手順は、Railway/Vercelのアカウント認証が必要なため、
ユーザー自身が実施する（Claude Codeがターミナル上で一緒に進めることは可能）。

## 前提

- Railwayアカウント・Vercelアカウントを保有していること
- `railway` CLI（`npm i -g @railway/cli` 等）でログイン済みであること

## 1. Railwayプロジェクトの作成とDBプラグイン追加

1. Railwayで新規プロジェクトを作成する
2. プロジェクトに「PostgreSQL」プラグインを追加する
3. プロジェクトに「Redis」プラグインを追加する

## 2. go-api サービスの追加

1. GitHubリポジトリ（`stock-anomaly-detection`）と連携してサービスを追加する
2. サービス設定で以下を指定する:
   - Root Directory: リポジトリルート（変更しない）
   - Dockerfile Path: `go-api/Dockerfile`
3. Settings → Healthcheck Path に `/health` を設定する
4. 環境変数タブで以下を設定する（`DATABASE_URL`・`REDIS_URL`はPostgres/Redisプラグインの
   「Variable Reference」機能で自動注入されるものを使う。`PORT`はRailwayが自動注入するため設定不要）:

   | 変数名 | 値 |
   |---|---|
   | `ANTHROPIC_API_KEY` | Anthropic Consoleで発行したAPIキー |
   | `SLACK_WEBHOOK_URL` | Slack Incoming Webhook URL |
   | `PYTHON_ENGINE_URL` | `http://<python-engineサービス名>.railway.internal:8000`（3節でサービス作成後に設定） |
   | `CLAUDE_MODEL` | 任意（未設定時のデフォルトは`claude-opus-5`） |
   | `JWT_SECRET` | ランダムな秘密文字列（`openssl rand -base64 32`等で生成） |
   | `ANOMALY_THRESHOLD` | 任意（未設定時のデフォルトは`2.5`） |
   | `POLL_TIME` | 任意（未設定時のデフォルトは`16:00`） |

## 3. python-engine サービスの追加

1. 同じGitHubリポジトリと連携してサービスを追加する
2. サービス設定で以下を指定する:
   - Root Directory: リポジトリルート（変更しない）
   - Dockerfile Path: `python-engine/Dockerfile`
3. このサービスは外部公開しない（go-apiからのプライベートネットワーキング経由の通信のみ）
4. サービス作成後に払い出される内部ホスト名（`<サービス名>.railway.internal`）を、
   2節の`PYTHON_ENGINE_URL`に反映する

## 4. DBマイグレーションの適用

Railway CLIでプロジェクトにリンクした状態で実行する:

```bash
railway link
railway run --service postgres psql "$DATABASE_URL" -f go-api/migrations/001_initial_schema.sql
```

## 5. go-api / python-engine のデプロイ確認

1. Railwayダッシュボードで両サービスのデプロイが成功していることを確認する
2. go-apiの公開URLに対して `curl https://<go-apiの公開URL>/health` を実行し、
   `{"status":"ok"}` が返ることを確認する

## 6. Vercelプロジェクトの作成

1. Vercelで同じGitHubリポジトリをインポートする
2. Root Directoryに `frontend` を設定する
3. Framework PresetはNext.jsが自動検出される（変更不要）
4. 環境変数タブで `GO_API_URL` に go-apiの公開URL（例: `https://go-api-production.up.railway.app`）を設定する
5. デプロイを実行する

## 7. e2e動作確認

1. Vercelのデプロイ済みURLにブラウザでアクセスする
2. `/register` でユーザー登録 → `/login` でログイン → `/watchlist` で銘柄追加、の一連のフローを
   実際に操作して確認する
3. 可能であれば、追加した銘柄の異常検知〜Slack通知までのフローも確認する
   （`ANOMALY_THRESHOLD`を一時的に低い値にする等、既存のe2e検証と同じ手法で発火させる）

## 以降の運用

`main`ブランチへのマージで、Railway・Vercelとも自動的に再ビルド・再デプロイされる。
追加のデプロイ操作は不要。
```

- [ ] **Step 2: コミット**

```bash
git add docs/deployment/phase5-railway-vercel-setup.md
git commit -m "docs: add Railway/Vercel deployment setup guide"
```
