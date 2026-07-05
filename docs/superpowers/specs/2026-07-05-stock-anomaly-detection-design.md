# 株価異常検知 × AI自動分析通知システム 設計書

**作成日**: 2026-07-05  
**ステータス**: 承認済み

---

## 1. プロダクト概要

日本株（東証）の異常な価格変動をリアルタイムで検知し、AIが自動分析してSlackへ通知するSaaSシステム。「監視→検知→分析→通知」を人間の介在なしに自律実行することが最大の価値。

**ターゲットユーザー**: 複数ユーザー対応のSaaS形式  
**ポートフォリオ用途**: フルスタック × クラウド知見を持つバックエンドエンジニア志望

---

## 2. 技術スタック

| レイヤー | 技術 | 理由 |
|----------|------|------|
| APIサーバー | Go 1.22 | goroutineによる並行処理で複数銘柄を同時監視 |
| 分析エンジン | Python 3.11 | pandas/numpyによるテクニカル指標計算 |
| DB | PostgreSQL（Railway） | リレーショナル構造に最適・RailwayネイティブPlugin |
| キャッシュ | Redis（Railway） | 株価キャッシュ・通知頻度制御 |
| フロントエンド | Next.js + TypeScript（Vercel） | 型安全・Vercelとの親和性 |
| AIレポート | Claude API | 前処理済みサマリーを渡してトークン最小化 |
| 株価データ | J-Quants API / 立花証券e支店API | JPX公式・開発環境は無料 |
| ニュース | Finnhub News API | 株価特化・無料枠あり |
| 通知 | Slack Webhook | シンプル実装・SaaSらしいUX |

---

## 3. リポジトリ構成

モノレポ構成。Railway・Vercelともにサブディレクトリデプロイ対応。

```
stock-anomaly-detection/
├── go-api/               # Go APIサーバー（Railway Service）
│   ├── cmd/
│   ├── internal/
│   └── Dockerfile
├── python-engine/        # Python分析エンジン（Railway Service）
│   ├── app/
│   └── Dockerfile
├── frontend/             # Next.js UI（Vercel）
│   ├── app/
│   └── public/
└── docs/
    ├── adr/              # Architecture Decision Records
    └── superpowers/specs/
```

---

## 4. システムアーキテクチャ

### データフロー

```
J-Quants API / 立花証券API
        ↓ 毎分（取引時間 9:00〜15:30）
  Go APIサーバー（Railway）
  ├─ goroutineで銘柄ごとに並行取得
  ├─ Redisに直近30件の終値をキャッシュ
  ├─ Z-score計算 → 2.5σ超えのみ次へ（事前フィルタリング）
  └─ 通知頻度制御（同一銘柄30分以内は再通知しない）
        ↓ 異常検知時のみ・同期HTTP
  Python 分析エンジン（Railway）
  ├─ RSI / MACD / ボリンジャーバンド計算（pandas/numpy）
  ├─ Finnhub News APIでニュース取得
  ├─ 前処理済みサマリーをClaude APIへ送信（200字レポート生成）
  └─ Slack Webhookで通知送信
        ↓
  PostgreSQL（通知履歴保存）

  Next.js UI（Vercel）
  └─ ユーザー登録・銘柄登録・閾値設定・通知履歴閲覧
```

### Go → Python 通信

**方式**: 同期HTTP  
GoがPythonの REST APIを直接呼び出し、分析完了まで待機してからレスポンスを受け取る。PythonはSlack通知まで実行し、結果（AI レポートテキスト）をGoへ返す。GoはPostgreSQLへ通知履歴を保存する。

### Z-score 事前フィルタリング

GoはRedisから直近30件の終値を取得して平均・標準偏差を計算し、現在値のZ-scoreが **デフォルト2.5σ** を超えた場合のみPythonへ送信する。ユーザーは銘柄ごとに`alert_threshold`を変更可能。

---

## 5. データモデル

```sql
-- ユーザー
CREATE TABLE users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email         TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  slack_webhook_url TEXT,
  created_at    TIMESTAMPTZ DEFAULT now()
);

-- 監視銘柄
CREATE TABLE watchlist (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
  stock_code      TEXT NOT NULL,
  alert_threshold NUMERIC DEFAULT 2.5,
  created_at      TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id, stock_code)
);

-- 通知履歴
CREATE TABLE notifications (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              UUID REFERENCES users(id),
  stock_code           TEXT NOT NULL,
  anomaly_score        NUMERIC NOT NULL,
  ai_report            TEXT,
  technical_indicators JSONB,
  slack_sent           BOOLEAN DEFAULT false,
  notified_at          TIMESTAMPTZ DEFAULT now()
);
```

---

## 6. Claude APIへのプロンプト設計

トークン最小化のため生データは渡さず、前処理済みサマリーのみを送信する。

```
銘柄：{stock_code}（{stock_name}）
異常スコア：{z_score}σ（過去30日比）
現在株価：{price}円（前日比 {change_pct}%）
出来高：通常比 {volume_ratio}%
RSI：{rsi}
MACD：{macd_signal}
直近ニュース要約：
{news_summary}
上記を踏まえ異常の原因とリスクを200字以内で分析してください。
```

---

## 7. 認証

- **方式**: JWT（有効期限24時間）
- **エンドポイント**: `POST /auth/register`、`POST /auth/login`
- すべての保護エンドポイントは`Authorization: Bearer <token>`ヘッダーを必須とする

---

## 8. エラーハンドリング

| シナリオ | 対応 |
|----------|------|
| J-Quants API 障害 | 最大3回リトライ（指数バックオフ）・立花証券APIへフォールバック |
| Python エンジン タイムアウト | 30秒でタイムアウト・エラーログ記録・Slack通知はスキップ |
| Slack Webhook 失敗 | 最大3回リトライ・失敗時はPostgreSQLに`slack_sent=false`で保存 |
| Claude API 失敗 | リトライ1回・失敗時はテクニカル指標のみのSlack通知にフォールバック |

---

## 9. 非機能要件

| 項目 | 要件 |
|------|------|
| 株価取得間隔 | 取引時間中（9:00〜15:30）は1分ごと |
| 異常検知〜通知の遅延 | 30秒以内 |
| 同時監視可能銘柄数 | 1ユーザーあたり最大50銘柄 |
| 稼働時間 | 取引時間中99%以上 |
| APIリトライ | 最大3回 |

---

## 10. 開発フェーズ

| フェーズ | 内容 | 目安 |
|----------|------|------|
| Phase 1 | Go + J-Quants API で株価取得・Z-score異常検知 | 2週間 |
| Phase 2 | Python分析エンジン（テクニカル指標・ニュース取得） | 1週間 |
| Phase 3 | Claude API連携・Slack通知 | 1週間 |
| Phase 4 | ユーザー管理・JWT認証・Next.js UI | 2週間 |
| Phase 5 | Docker化・Railwayデプロイ・テスト | 1週間 |
| **合計** | | **7週間** |

---

## 11. インフラ構成

```
Railwayプロジェクト
├── go-api（Service）        ← Go APIサーバー
├── python-engine（Service）← Python分析エンジン
├── PostgreSQL（Plugin）     ← DB
└── Redis（Plugin）          ← キャッシュ・頻度制御

Vercel
└── frontend/               ← Next.js
```

---

## 12. Goディレクトリ構成（Clean Architecture + 実践的DDD）

依存の方向は常に外側 → 内側（domain層は外部依存ゼロ）。

```
go-api/
├── cmd/
│   └── api/
│       └── main.go               # DI組み立て・サーバー起動
└── internal/
    ├── domain/                   # ドメイン層（外部依存なし）
    │   ├── user/
    │   │   ├── entity.go         # User エンティティ
    │   │   └── repository.go     # UserRepository インターフェース
    │   ├── watchlist/
    │   │   ├── entity.go         # Watchlist エンティティ
    │   │   └── repository.go
    │   ├── notification/
    │   │   ├── entity.go         # Notification エンティティ
    │   │   └── repository.go
    │   └── anomaly/
    │       ├── service.go        # Z-scoreドメインサービス
    │       └── value_object.go   # AnomalyScore, ZScore 値オブジェクト
    ├── usecase/                  # ユースケース層（アプリケーションサービス）
    │   ├── monitor_stocks.go     # 株価監視・異常検知ユースケース
    │   ├── register_user.go      # ユーザー登録
    │   └── manage_watchlist.go   # 銘柄登録・削除・閾値変更
    ├── interface/                # インターフェースアダプター層
    │   ├── handler/
    │   │   ├── auth_handler.go   # 認証エンドポイント
    │   │   └── watchlist_handler.go
    │   └── gateway/
    │       ├── jquants_client.go      # J-Quants API クライアント
    │       └── python_engine_client.go # Python分析エンジン HTTPクライアント
    └── infrastructure/           # インフラ層（外部依存の実装）
        ├── persistence/
        │   ├── user_repository.go         # PostgreSQL実装
        │   └── notification_repository.go
        └── cache/
            └── redis_stock_cache.go       # Redisキャッシュ実装
```

**依存ルール**:
- `domain` はGoの標準ライブラリのみ使用
- `usecase` は `domain` のみに依存
- `interface` / `infrastructure` は `usecase` と `domain` に依存
- `main.go` でDIコンテナを手動組み立て（Wire等は使わずシンプルに）

---

## 13. テストハーネス設計

### Go

| 層 | テスト種別 | 方針 |
|----|-----------|------|
| `domain/` | ユニットテスト | 外部依存ゼロのため純粋なテーブルドリブンテスト |
| `usecase/` | ユニットテスト | リポジトリインターフェースを `testify/mock` でモック |
| `interface/handler/` | ユニットテスト | `httptest` でハンドラ単体テスト |
| `infrastructure/` | integrationテスト | GitHub Actions の `services: postgres/redis` で実DB起動 |
| `interface/gateway/` | ユニットテスト | J-Quants・Python engineはインターフェースを切ってモック |

### Python

- `pytest` + `pytest-mock`
- Finnhub News API・Claude APIはモック
- テクニカル指標計算は既知の入力→期待値でテーブルドリブン形式

### Next.js

- Jest + React Testing Library でコンポーネントテスト

### CI（GitHub Actions）

```yaml
# .github/workflows/test.yml（概略）
services:
  postgres:
    image: postgres:16
  redis:
    image: redis:7

jobs:
  go-test:    # go test ./...
  python-test: # pytest
  frontend-test: # jest
```

integrationテストはGitHub Actions上で実DB（PostgreSQL・Redis）を立ち上げて実行。testcontainersは使わずActionsのservicesを利用する。

---

## 14. コーディング規約

- コミットメッセージ: Conventional Commits（`feat:`, `fix:`, `chore:` など）
- Goテスト: table driven tests
- APIキー: 環境変数で管理。コードへの直書き禁止
- Go・Python間通信: 同期HTTP
