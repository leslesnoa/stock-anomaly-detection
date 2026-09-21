# Phase 5: Docker化・Railway/Vercelデプロイ — 設計仕様

**ステータス**: 承認済み
**日付**: 2026-09-21

## 背景

ADR-003（`docs/adr/ADR-003-railway-vercel-infra.md`）でインフラ方針
（バックエンド: Railway、フロントエンド: Vercel）は決定済みだが、設定ファイルは未着手。
Phase 1〜4.5・各種フォローアップPR（#4〜#15）はすべてローカル/CI環境（`go test`・`uv run pytest`・
`docker-compose.yml`のpostgres/redisのみ）で完結しており、本番デプロイの仕組みは一切存在しない。

本タスクでは、既存の3コンポーネント（go-api・python-engine・frontend）を Railway + Vercel に
実際にデプロイできる状態にする。

## スコープ

**含む:**
- `go-api`用Dockerfileの新規作成
- `go-api`への`GET /health`エンドポイント追加（Railwayヘルスチェック用）
- `frontend/lib/go-api-client.ts`の`GO_API_URL`未設定ガードを、ビルド時に発火しない形に変更
  （[[project_frontend_followup_build_time_go_api_url]]の解消）
- Railway上の3サービス構成（go-api・python-engine・PostgreSQL・Redis）のセットアップ手順の確立
- Vercel上のfrontendデプロイのセットアップ手順の確立
- 本番DBへの初回マイグレーション適用手順の確立
- 本番環境変数一覧の整理・ドキュメント化

**含まない（意図的に対象外）:**
- ステージング環境（本番環境のみのシンプルな構成とする）
- DBマイグレーション自動化ツール（golang-migrate等）の導入
  （[[project_followup_db_migration_tooling]]。マイグレーションファイルが1つのみの現状ではオーバースペック）
- GitHub Actions経由でのデプロイ制御（Railway/VercelのネイティブGit連携に任せる）
- E2Eテスト・ステージング上での自動検証（[[project_frontend_followup_e2e_testing]]は別タスク）
- `middleware.ts`→`proxy.ts`移行（[[project_frontend_followup_middleware_proxy_migration]]は別タスク）

## アーキテクチャ

```
GitHub (main ブランチ)
  │
  ├─ push/merge ──▶ Railway (Git連携・自動ビルド/デプロイ)
  │                    ├─ service: go-api        (Dockerfile: go-api/Dockerfile)
  │                    ├─ service: python-engine  (Dockerfile: python-engine/Dockerfile, 既存)
  │                    ├─ plugin: PostgreSQL
  │                    └─ plugin: Redis
  │                    go-api ⇄ python-engine は Railway プライベートネットワーキング
  │                    (*.railway.internal) で通信
  │
  └─ push/merge ──▶ Vercel (Git連携・自動ビルド/デプロイ)
                       └─ frontend/ (Next.js, Dockerfile不要)
                       frontend ─(サーバーサイドfetch)─▶ go-api (Railwayの公開URL)
```

frontendはBFFとしてGo APIをサーバーサイドから叩く設計（Phase 4フロントエンド設計を踏襲）のため、
ブラウザから直接Railwayへの通信は発生せず、CORS設定は不要。

## コンポーネント変更

### 1. `go-api/Dockerfile`（新規）

`python-engine/Dockerfile`と同じパターン（リポジトリルートをビルドコンテキストとして
`COPY go-api/...`する形）で新規作成する。マルチステージビルド（builderでコンパイル、
実行イメージはdistroless or alpine）で最終イメージを小さく保つ。

```dockerfile
FROM golang:1.24 AS builder
WORKDIR /src
COPY go-api/go.mod go-api/go.sum ./
RUN go mod download
COPY go-api/ ./
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api

FROM alpine:3.20
COPY --from=builder /bin/api /bin/api
EXPOSE 8080
CMD ["/bin/api"]
```

（`ca-certificates`パッケージがYahoo Finance/Claude API等への外部HTTPS通信に必要な場合は
alpineイメージに追加する。実装タスクで動作確認する。）

### 2. `go-api`: `GET /health`エンドポイント

`internal/interface/handler`に新規ハンドラを追加し、`main.go`の`mux`に登録する。
DB/Redis接続は確認せず、プロセスが生きていれば200を返す単純なlivenessチェックとする
（DB/Redis依存のreadinessチェックは、依存先の一時的な不調でRailwayが正常なコンテナを
再起動し続ける事態を避けるため、今回は導入しない）。

### 3. `python-engine/Dockerfile`

変更なし。既にPORT環境変数対応・uv frozenビルド済みで、そのままRailwayで使える。

### 4. `frontend/lib/go-api-client.ts`

モジュールトップレベルのIIFEで即座に評価している`GO_API_URL`未設定ガードを、
初回API呼び出し時に評価する遅延チェックに変更する。

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

各関数内で`GO_API_URL`定数を参照している箇所を`getGoApiUrl()`呼び出しに置き換える。
これにより`next build`時（静的解析のみでAPIは呼ばれない）はガードが発火せず、
本番ランタイムで実際にAPI呼び出しが発生した時点で未設定を検知する、という本来の意図は保たれる。

## Railway構成

### サービス

| サービス | 種別 | ビルド | 公開 |
|----------|------|--------|------|
| go-api | Dockerfile | `go-api/Dockerfile`（Root Directory: リポジトリルート） | 公開URL（frontendから参照） |
| python-engine | Dockerfile | `python-engine/Dockerfile`（既存、Root Directory: リポジトリルート） | 非公開（go-apiからのみ内部通信） |
| PostgreSQL | マネージドプラグイン | - | 非公開（go-apiからのみ） |
| Redis | マネージドプラグイン | - | 非公開（go-apiからのみ） |

各DockerfileサービスはRailwayの「Root Directory」をリポジトリルートのままにし、
`dockerfilePath`設定でそれぞれ`go-api/Dockerfile`・`python-engine/Dockerfile`を指定する
（サブディレクトリをRoot Directoryにするとビルドコンテキストが変わり、既存Dockerfileの
`COPY python-engine/...`という書き方と食い違うため）。

### 環境変数（go-api、Railway側で設定）

`DATABASE_URL`・`REDIS_URL`はRailwayのPostgres/Redisプラグインが自動的に注入する接続文字列を使う。
それ以外はRailwayダッシュボードで手動設定（値そのものはこのタスクの範囲外、ユーザーが用意する）:

`ANTHROPIC_API_KEY`、`SLACK_WEBHOOK_URL`、`PYTHON_ENGINE_URL`
（`http://python-engine.railway.internal:8000`形式の内部URL）、`CLAUDE_MODEL`（任意）、
`JWT_SECRET`、`ANOMALY_THRESHOLD`（任意）、`POLL_TIME`（任意）、`PORT`はRailwayが自動注入するため設定不要。

### ヘルスチェック

Railwayのヘルスチェックパスを`/health`に設定する。

## Vercel構成

- Root Directoryを`frontend/`に設定
- 環境変数: `GO_API_URL`（Railway上のgo-api公開URL）
- Framework PresetはNext.jsを自動検出させる（追加設定不要）

## DBマイグレーション運用

初回デプロイ前に、Railway CLI経由で手動適用する。

```
railway run --service postgres psql $DATABASE_URL -f go-api/migrations/001_initial_schema.sql
```

手順をREADME（または`docs/`配下）に残す。将来マイグレーションファイルが増えてきたタイミングで
自動化ツールの導入を検討する（[[project_followup_db_migration_tooling]]）。

## デプロイフロー（初回セットアップ手順の骨子）

1. Railwayで新規プロジェクトを作成し、PostgreSQL・Redisプラグインを追加
2. go-api・python-engineサービスをGitHubリポジトリ連携で追加し、それぞれ`dockerfilePath`を設定
3. 環境変数を設定（上記一覧）
4. DBマイグレーションを手動適用
5. go-api・python-engineをデプロイし、`/health`が200を返すことを確認
6. Vercelで新規プロジェクトを作成し、Root Directoryを`frontend/`に設定、`GO_API_URL`を設定してデプロイ
7. 実際にブラウザから register→login→watchlist追加のフローを通して動作確認

以降は`main`へのマージで各サービスが自動的に再デプロイされる。

**実行主体の区分:** 上記1〜4（プロジェクト作成・環境変数設定・DBマイグレーション適用）は
Railway/Vercelのアカウント認証情報が必要な操作であり、Claude Codeが自律的に実行することはできない。
ユーザーが手順書に従って実施し、必要であればCLIコマンドの実行やトラブルシューティングを
ターミナル上で一緒に進める。本タスクの実装計画がスコープとするのは、コード変更
（`go-api/Dockerfile`・`/health`エンドポイント・`go-api-client.ts`修正）と、
上記手順を明文化したセットアップ手順書（`docs/`配下）の作成までとする。
5〜7（デプロイ実行・動作確認）はコード変更PRのマージ後、ユーザーと一緒に進める。

## テスト・検証方針

- `go-api/Dockerfile`: ローカルで`docker build`→`docker run`し、`/health`が200を返すことを確認
- 新規`/health`ハンドラ: 通常のGoユニットテスト（`httptest`）で200を返すことを検証
- `go-api-client.ts`の変更: 既存のVitestテストがビルド後も壊れていないことを確認
  （`next build`がGO_API_URL未設定でも成功することを確認）
- 実デプロイ後: Railway/Vercelのダッシュボード上でビルドログ・デプロイステータスを確認し、
  実際にブラウザからregister→login→watchlist追加→（可能であれば）異常検知〜Slack通知までの
  e2eフローを通して動作確認する

## エラーハンドリング

- `/health`: プロセスが生きている限り常に200を返す。DB/Redis接続不能時もgo-apiプロセス自体は
  クラッシュしない設計（`main.go`は起動時に`pool.Ping`でfail-fastするため、接続不能ならそもそも
  プロセスが起動せずRailwayのデプロイが失敗する。起動後の一時的な接続断は既存のリトライ・
  フォールバック処理に委ねる）
