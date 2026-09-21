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
