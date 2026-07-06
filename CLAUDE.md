# 株価異常検知 × AI自動分析通知システム

## 作業フロー（厳守）
実装作業は必ず以下の順序で行うこと。順序を飛ばしてはいけない。

1. **ブランチ作成** — `git checkout -b feature/<task-name>`（mainで直接作業禁止）
2. **プラン作成** — `superpowers:writing-plans` スキルで実装計画を作成・承認を得る
3. **実装** — 承認済みプランに従って実装する

> ハーネスはmainブランチでのコードファイル編集を自動的にブロックします。

## プロジェクト構造
- モノレポ: `go-api/`（Goサーバー）、`python-engine/`（将来）、`frontend/`（将来）
- Goモジュール: `github.com/stock-anomaly-detection/go-api`（`go-api/`内で操作）

## Goコマンド（`go-api/`ディレクトリから実行）
- `go test -race -short ./...` — 単体テスト（外部サービス不要）
- `go test -race ./...` — 統合テスト（DATABASE_URL + REDIS_URL 必要）
- `go build ./...` — ビルド確認

## アーキテクチャ
- Clean Architecture + Pragmatic DDD（bounded context・domain eventsなし）
- 依存方向: `infrastructure/usecase` → `domain`（逆方向禁止）
- リポジトリインターフェースはdomainパッケージ内に定義

## 重要な設計決定
- DBスキーマ: UUID PK（`gen_random_uuid()`）、`int64`不使用
- `WatchlistRepository.FindByUserID(ctx, userID string)` — userIDはUUID文字列
- Redisキー: `price:<4桁コード>`、スライディングウィンドウ30件
- J-Quants API V2: 認証は `x-api-key` ヘッダー、エンドポイント `/v2/equities/bars/daily`、レスポンス `data[].C`（終値）
- J-Quants APIコード: 4桁→5桁（末尾0追加）

## テスト方針
- 統合テスト: `testing.Short()` または環境変数未設定でスキップ
- J-Quantsクライアント: `httptest.NewServer`でモック（実API呼び出しなし）
- `-race`フラグ必須（JQuantsClientのidTokenはsync.Mutexで保護済み）

## GitHub Actions CI
- postgres:16 + redis:7 サービス
- `DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`
- `REDIS_URL: redis://localhost:6379`

## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）
