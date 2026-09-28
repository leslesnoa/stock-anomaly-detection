# Railway / Vercel デプロイセットアップ手順

設計: `docs/superpowers/specs/2026-09-21-phase5-docker-railway-deploy-design.md`

このドキュメントの手順は、Railway/Vercelのアカウント認証が必要なため、
ユーザー自身が実施する（Claude Codeがターミナル上で一緒に進めることは可能）。

## 前提

- Railwayアカウント・Vercelアカウントを保有していること
- `railway` CLI（`npm i -g @railway/cli` 等）でログイン済みであること
- ローカルに`psql`（PostgreSQLクライアント）がインストールされていること（4節のマイグレーション適用で使用）

## 1. Railwayプロジェクトの作成とDBプラグイン追加

1. Railwayで新規プロジェクトを作成する
2. プロジェクトに「PostgreSQL」プラグインを追加する

## 2. go-api サービスの追加

1. GitHubリポジトリ（`stock-anomaly-detection`）と連携してサービスを追加する
2. サービス設定で以下を指定する:
   - Root Directory: リポジトリルート（変更しない）
   - Dockerfile Path: `go-api/Dockerfile`
3. Settings → Healthcheck Path に `/health` を設定する
4. Settings → Networking → Public Networking → 「Generate Domain」で公開URLを発行する
   （Railwayはデフォルトではサービスを外部公開しないため、この操作が必要。ここで発行される
   URLを5節のヘルスチェックのcurl、6節の`GO_API_URL`で使用する）
5. 環境変数タブで以下を設定する（`DATABASE_URL`はPostgresプラグインの
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
5. 環境変数タブで`PORT`を明示的に`8000`に設定する（Railwayは通常`PORT`を自動注入するが、
   それに任せると`python-engine/Dockerfile`のuvicornが別ポートで待受を始め、
   2節の`PYTHON_ENGINE_URL`にハードコードした`:8000`と不整合になる。自動注入を上書きして
   `8000`に固定することで、このズレを防ぐ）

### トラブルシューティング: go-apiからpython-engineに到達できない場合

症状: Slack通知は届くがテクニカル指標のみでAI分析が一度も出ない
（go-apiのログに`python-engine.railway.internal`への接続エラーが出ていないか確認する）。

考えられる原因: Railwayのプライベートネットワーク（`*.railway.internal`）はIPv6のみで
疎通する一方、`python-engine/Dockerfile`のuvicornは`--host 0.0.0.0`（IPv4）で待受しており、
go-apiからの内部通信が黙って失敗している可能性がある。この場合、`AnalyzeAndNotifyUsecase`の
フォールバック仕様（CLAUDE.md参照）によりClaude分析なしのテクニカル指標のみの通知に
静かに縮退するため、エラーとして気づきにくい。

**2026-09-28、この懸念が実際に発生し確認・修正済み:** PR-B（`/stocks/{code}/chart`）で
python-engineへの同期呼び出しが増えたことで顕在化し、`python-engine/Dockerfile`のuvicorn起動を
`--host ::`（IPv6ワイルドカード）に変更した。あわせて、go-api側の`GetStockChartUsecase`にも
python-engine呼び出し専用の10秒タイムアウト（`forecastTimeout`）を追加し、疎通失敗時に
go-apiの`http.Server.WriteTimeout`（30秒）が先に発火して接続が強制切断される事態を防いでいる。

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

> **`dirty`状態からの回復について:** `schema_migrations`テーブルの`dirty`カラムが`true`の場合、
> マイグレーションが途中で失敗した状態を意味する。`dirty=true`の間はgo-apiが起動しない
> （`RunMigrations`が毎回失敗し`log.Fatalf`でプロセスが終了する）ため、再起動や再デプロイだけ
> では回復しない。回復するには、原因になったマイグレーションのSQLを確認し、DBの実際の状態
> （どこまで適用されたか）を手動で確認した上で、
> `UPDATE schema_migrations SET dirty = false WHERE version = <実際に完了しているバージョン>;`
> （または`golang-migrate`の`migrate force <version>` CLI）でdirtyフラグを解除してから
> 再デプロイする。

## 5. go-api / python-engine のデプロイ確認

1. Railwayダッシュボードで両サービスのデプロイが成功していることを確認する
2. go-apiの公開URLに対して `curl https://<go-apiの公開URL>/health` を実行し、
   `{"status":"ok"}` が返ることを確認する

## 6. Vercelプロジェクトの作成

1. Vercelで同じGitHubリポジトリをインポートする
2. Root Directoryに `frontend` を設定する
3. Framework PresetはNext.jsが自動検出される（変更不要）
4. 環境変数タブで `GO_API_URL` に go-apiの公開URL（例: `https://go-api-production.up.railway.app`）を設定する。
   VercelのPreviewデプロイも`NODE_ENV=production`でビルドされるため、`GO_API_URL`は
   Production環境だけでなくPreview環境にも設定すること（Preview限定で未設定だと、
   該当デプロイは一見正常に見えるがAPI呼び出し時にエラーになる）
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
