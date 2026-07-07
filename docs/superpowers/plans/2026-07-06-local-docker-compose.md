# Local Docker Compose Environment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Docker Compose でローカル開発環境（PostgreSQL 16 + Redis 7）を一発起動できるようにする。

**Architecture:** `docker-compose.yml` で postgres:16 と redis:7 を定義し、migrations ディレクトリを `/docker-entrypoint-initdb.d` にマウントすることで初回起動時に自動マイグレーションを適用する。`.env` から環境変数を読み込み、`Makefile` で操作を統一する。

**Tech Stack:** Docker Compose v2, PostgreSQL 16, Redis 7, GNU Make

## Global Constraints

- PostgreSQL イメージ: `postgres:16`（CI と同バージョン）
- Redis イメージ: `redis:7`（CI と同バージョン）
- DB 名: `stock_anomaly`、ユーザー: `postgres`、パスワード: `postgres`
- ホストポート: PostgreSQL `5432`、Redis `6379`
- Go コマンドは `go-api/` ディレクトリ内で実行すること
- `.env` は `.gitignore` に追加し、`.env.example` のみリポジトリに含める

---

### Task 1: docker-compose.yml + .env.example + .gitignore

**Files:**
- Create: `docker-compose.yml`（プロジェクトルート）
- Create: `.env.example`（プロジェクトルート）
- Create: `.gitignore`（プロジェクトルート）

**Interfaces:**
- Produces: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable`（Task 2 で使用）
- Produces: `REDIS_URL=redis://localhost:6379`（Task 2 で使用）

- [ ] **Step 1: docker-compose.yml を作成する**

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: stock_anomaly
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./go-api/migrations:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
  redis_data:
```

- [ ] **Step 2: .env.example を作成する**

```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable
REDIS_URL=redis://localhost:6379
JQUANTS_EMAIL=your-email@example.com
JQUANTS_PASSWORD=your-jquants-password
STOCK_CODES=7203,6758,9984
ANOMALY_THRESHOLD=2.5
```

- [ ] **Step 3: .gitignore を作成する**

```
.env
```

- [ ] **Step 4: コンテナを起動して動作確認する**

```bash
docker compose up -d
```

期待: postgres と redis の 2 コンテナが起動し、ステータスが `healthy` になる。

```bash
docker compose ps
```

期待出力（抜粋）:
```
NAME                            STATUS
stock-anomaly-detection-postgres-1   Up ... (healthy)
stock-anomaly-detection-redis-1      Up ... (healthy)
```

- [ ] **Step 5: マイグレーション適用を確認する**

```bash
docker compose exec postgres psql -U postgres -d stock_anomaly -c "\dt"
```

期待出力（抜粋）:
```
 public | notifications | table | postgres
 public | users         | table | postgres
 public | watchlist     | table | postgres
```

- [ ] **Step 6: コミットする**

```bash
git add docker-compose.yml .env.example .gitignore
git commit -m "feat: add docker-compose for local development (postgres:16 + redis:7)"
```

---

### Task 2: Makefile + ローカル起動確認

**Files:**
- Create: `Makefile`（プロジェクトルート）

**Interfaces:**
- Consumes: `docker-compose.yml`（Task 1 で作成）
- Consumes: `.env.example` → `.env`（ユーザーが実際の認証情報を設定）

- [ ] **Step 1: .env ファイルを用意する（ユーザー作業）**

```bash
cp .env.example .env
# .env を編集して JQUANTS_EMAIL / JQUANTS_PASSWORD / STOCK_CODES を実際の値に変更する
```

- [ ] **Step 2: Makefile を作成する**

```makefile
.PHONY: up down logs run test

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

run: up
	cd go-api && export $$(grep -v '^#' ../.env | xargs) && go run ./cmd/api/main.go

test:
	cd go-api && go test -race -short ./...
```

- [ ] **Step 3: make up でコンテナを起動できることを確認する**

```bash
make up
```

期待: `docker compose up -d` と同じ動作。

- [ ] **Step 4: make test で短期テストが通ることを確認する**

```bash
make test
```

期待: `go test -race -short ./...` が PASS する。

- [ ] **Step 5: make run でアプリが起動することを確認する（.env 設定後）**

```bash
make run
```

期待ログ（抜粋）:
```
monitoring N stocks (threshold=2.5σ)
```

- [ ] **Step 6: コミットする**

```bash
git add Makefile
git commit -m "feat: add Makefile for local dev (make up/down/run/test)"
```
