# ニュースセンチメント追加修正（基準日・基準終値列／名前変更／コメント整備）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** PR #27（`feature/news-sentiment`）に、スコアの事後検証に必要な基準日・基準終値の保存、第三者に分かりづらい名前3件の変更、誤ったコメントの修正と必要なコメントの追加を入れる。

**Architecture:** 既存のクリーンアーキテクチャのまま。`stock_sentiment_snapshots` に nullable 列2つを新マイグレーション 005 で追加し、domain `Snapshot` → persistence → usecase の順に値を通す。名前変更は Go の識別子のみで、JSON キー・DB 列名は変えない。

**Tech Stack:** Go 1.x / pgx v5 / golang-migrate / testify、Next.js 16（コメント1箇所のみ）

**Spec:** `docs/superpowers/specs/2026-09-30-news-sentiment-design.md`（本計画はその追加修正。スコア精度の検証ロジック自体は spec 278 行目どおり対象外で、本計画は検証に必要な値を保存するところまで）

## Global Constraints

- Go コマンドは `go-api/` から実行。テストは必ず `-race`。
- 統合テスト: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/`
- マイグレーション 004 は編集しない（ローカルDBに適用済みで、golang-migrate は編集後の 004 を再実行しないため）。追加は `005_*.up.sql` / `005_*.down.sql` のペア。
- `.down.sql` は `IF EXISTS` で冪等にする（docker-compose initdb では down が up より先に走る）。
- JSON キー（`short_term_up_probability`, `sentiment_confidence` 等）と DB 列名（`checked_at` 含む）は変更しない。
- コメント方針: 原則書かない。書くのは自明でない「なぜ」だけで、日本語・短く。
- 動作変更は Task 1 のみ。Task 2・3 はビルド結果が同じになる変更（識別子・コメント・プロンプト文言1箇所）。
- コミットメッセージ末尾: 空行 + `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`

## Review Focus

1. 株価が1件も無い銘柄（バックフィル失敗直後）→ `base_price_date`/`base_close` は NULL で保存され、読み出しても panic しない。→ Task 1 の usecase テスト（quotes 空）と persistence テスト（NULL 往復）で固定。
2. 入力が変わらず touch だけの経路 → 既存スナップショットの基準日・終値がそのまま返る（`now` の日付などで上書きしない）。→ Task 1 の touch テストに検証を追加。
3. マイグレーション 005 を2回流しても・down を先に流しても失敗しない。→ SQL を `IF NOT EXISTS` / `IF EXISTS` で書く（Task 1）。
4. 既存の行（005 以前に保存されたスナップショット）は新列が NULL → `FindLatestSnapshot` が NULL を受けられる（ポインタでスキャン）。→ Task 1 の NULL 往復テストで兼ねる。
5. `NUMERIC` の終値（例 1234.5）が float64 で誤差なく往復する。→ Task 1 の persistence テストで小数を使う。

---

### Task 1: スナップショットに基準日・基準終値を保存する

**Files:**
- Create: `go-api/migrations/005_add_snapshot_base_price.up.sql`
- Create: `go-api/migrations/005_add_snapshot_base_price.down.sql`
- Modify: `go-api/internal/domain/sentiment/sentiment.go`（`Snapshot` 型）
- Modify: `go-api/internal/infrastructure/persistence/sentiment_repository.go`（`FindLatestSnapshot`, `InsertSnapshot`）
- Modify: `go-api/internal/usecase/get_news_sentiment.go`（`refresh`）
- Test: `go-api/internal/infrastructure/persistence/sentiment_repository_test.go`
- Test: `go-api/internal/usecase/get_news_sentiment_test.go`

**Interfaces:**
- Produces: `sentiment.Snapshot` に `BasePriceDate string`（"YYYY-MM-DD"、空文字なら DB は NULL）と `BaseClose *float64` を追加。

- [ ] **Step 1: マイグレーション 005 を作成**

`go-api/migrations/005_add_snapshot_base_price.up.sql`:
```sql
-- スコア算出時点の最新終値の日付と値。short_term_up_probability が当たったかを後から検証するために残す。
ALTER TABLE stock_sentiment_snapshots ADD COLUMN IF NOT EXISTS base_price_date DATE;
ALTER TABLE stock_sentiment_snapshots ADD COLUMN IF NOT EXISTS base_close NUMERIC;
```

`go-api/migrations/005_add_snapshot_base_price.down.sql`:
```sql
-- 001_initial_schema.down.sqlと同じ理由でIF EXISTSを使う。
ALTER TABLE IF EXISTS stock_sentiment_snapshots DROP COLUMN IF EXISTS base_close;
ALTER TABLE IF EXISTS stock_sentiment_snapshots DROP COLUMN IF EXISTS base_price_date;
```

- [ ] **Step 2: domain の `Snapshot` に2フィールドを追加**

`go-api/internal/domain/sentiment/sentiment.go` の `Snapshot` を次に置き換える:
```go
type Snapshot struct {
	ID               string
	StockCode        string
	Scores           *StockScores // 対象記事0件ならnil
	ArticleCount     int
	InputFingerprint string
	ScoredBy         string
	// BasePriceDate / BaseClose はスコア算出時点の最新終値（株価が無ければ "" / nil）。
	// ShortTermUpProbability の答え合わせの基準になる。
	BasePriceDate string
	BaseClose     *float64
	CreatedAt     time.Time
	CheckedAt     time.Time
}
```
（`ShortTermUpProbability` という名前は Task 2 で導入する。Task 1 の時点ではコメント内の名前だけ先行してよい。）

- [ ] **Step 3: persistence の失敗するテストを書く**

`sentiment_repository_test.go` の `TestPgSentimentRepository_Snapshots` で、`newer` のスナップショットの Insert を次に変え、`assert.Equal(t, "fp-1", latest.InputFingerprint)` の直後に検証を追加する:
```go
	baseClose := 1234.5
	id, err := repo.InsertSnapshot(ctx, sentiment.Snapshot{StockCode: "7203", Scores: &scores, ArticleCount: 3, InputFingerprint: "fp-1", ScoredBy: "claude",
		BasePriceDate: "2026-09-29", BaseClose: &baseClose, CreatedAt: newer, CheckedAt: newer})
```
```go
	assert.Equal(t, "2026-09-29", latest.BasePriceDate)
	require.NotNil(t, latest.BaseClose)
	assert.Equal(t, 1234.5, *latest.BaseClose)
```
`TestPgSentimentRepository_SnapshotWithoutScores` の末尾に追加:
```go
	assert.Equal(t, "", latest.BasePriceDate, "株価が無い時の基準日はNULLで保存される")
	assert.Nil(t, latest.BaseClose)
```

- [ ] **Step 4: テストが失敗することを確認**

Run: `DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test -race ./internal/infrastructure/persistence/ -run TestPgSentimentRepository_Snapshot`
Expected: FAIL（`latest.BasePriceDate` が空）

- [ ] **Step 5: persistence を実装**

`sentiment_repository.go` の `FindLatestSnapshot` を次に置き換える（`dateLayout` は同パッケージの `price_repository.go` で定義済み）:
```go
func (r *PgSentimentRepository) FindLatestSnapshot(ctx context.Context, stockCode string) (*sentiment.Snapshot, error) {
	var s sentiment.Snapshot
	var bullish, bearish, impact, confidence, shortTermUp *int
	var basePriceDate *time.Time
	err := r.conn.QueryRow(ctx,
		`SELECT id::text, stock_code, bullish_score, bearish_score, impact_score, confidence_score,
		        short_term_up_probability, article_count, input_fingerprint, scored_by,
		        base_price_date, base_close::float8, created_at, checked_at
		 FROM stock_sentiment_snapshots
		 WHERE stock_code = $1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		stockCode).Scan(&s.ID, &s.StockCode, &bullish, &bearish, &impact, &confidence, &shortTermUp,
		&s.ArticleCount, &s.InputFingerprint, &s.ScoredBy, &basePriceDate, &s.BaseClose, &s.CreatedAt, &s.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest sentiment snapshot %s: %w", stockCode, err)
	}
	if basePriceDate != nil {
		s.BasePriceDate = basePriceDate.Format(dateLayout)
	}
	if bullish != nil && bearish != nil && impact != nil && confidence != nil && shortTermUp != nil {
		s.Scores = &sentiment.StockScores{
			Bullish: *bullish, Bearish: *bearish, Impact: *impact,
			Confidence: *confidence, ShortTermUp: *shortTermUp,
		}
	}
	return &s, nil
}
```
`InsertSnapshot` を次に置き換える:
```go
func (r *PgSentimentRepository) InsertSnapshot(ctx context.Context, s sentiment.Snapshot) (string, error) {
	var bullish, bearish, impact, confidence, shortTermUp *int
	if s.Scores != nil {
		bullish, bearish, impact = &s.Scores.Bullish, &s.Scores.Bearish, &s.Scores.Impact
		confidence, shortTermUp = &s.Scores.Confidence, &s.Scores.ShortTermUp
	}
	var basePriceDate *time.Time
	if s.BasePriceDate != "" {
		d, err := time.Parse(dateLayout, s.BasePriceDate)
		if err != nil {
			return "", fmt.Errorf("insert sentiment snapshot %s: parse base price date: %w", s.StockCode, err)
		}
		basePriceDate = &d
	}
	var id string
	err := r.conn.QueryRow(ctx,
		`INSERT INTO stock_sentiment_snapshots
		   (stock_code, bullish_score, bearish_score, impact_score, confidence_score,
		    short_term_up_probability, article_count, input_fingerprint, scored_by,
		    base_price_date, base_close, created_at, checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id::text`,
		s.StockCode, bullish, bearish, impact, confidence, shortTermUp,
		s.ArticleCount, s.InputFingerprint, s.ScoredBy, basePriceDate, s.BaseClose, s.CreatedAt, s.CheckedAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert sentiment snapshot %s: %w", s.StockCode, err)
	}
	return id, nil
}
```

- [ ] **Step 6: persistence テストが通ることを確認**

Run: Step 4 と同じコマンド
Expected: PASS

- [ ] **Step 7: usecase の失敗するテストを書く**

`get_news_sentiment_test.go`:
1. `TestGetNewsSentiment_NewArticlesScoresOnlyUnscoredAndInsertsSnapshot` の `InsertSnapshot` matcher の条件末尾に追加:
```go
			s.BasePriceDate == quotes[29].Date && s.BaseClose != nil && *s.BaseClose == float64(quotes[29].Price) &&
```
（`s.CreatedAt.Equal(sentimentNow) && ...` の前に挿入し、`&&` の連結を保つ。）
2. `TestGetNewsSentiment_ExpiredCacheWithSameInputOnlyTouches` で `prev` に `BasePriceDate: "2026-09-29", BaseClose: &prevClose`（`prevClose := 1010.0` を `prev` の前に宣言）を足し、末尾に追加:
```go
	assert.Equal(t, "2026-09-29", got.Snapshot.BasePriceDate, "touchだけの時は前回の基準日を保つ")
	assert.Equal(t, &prevClose, got.Snapshot.BaseClose)
```
3. 株価0件の新規テストを追加（ファイル末尾）。既存テストのヘルパー（`newSentimentDeps`, `scoredArticle`, `d.usecase`）を使う:
```go
func TestGetNewsSentiment_NoPricesSavesEmptyBasePrice(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{scoredArticle("a", "1")}
	scores := sentiment.StockScores{Bullish: 50, Bearish: 50, Impact: 50, Confidence: 50, ShortTermUp: 50}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return([]stock.Quote{}, nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.MatchedBy(func(s sentiment.Snapshot) bool {
		return s.BasePriceDate == "" && s.BaseClose == nil
	})).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	_, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	d.repo.AssertExpectations(t)
}
```
（Task 2 で `UpsertArticles`/`ShortTermUp` はリネームされる。Task 1 時点では現行名で書く。）

- [ ] **Step 8: テストが失敗することを確認**

Run: `go test -race -short ./internal/usecase/ -run TestGetNewsSentiment`
Expected: FAIL（matcher 不一致で `InsertSnapshot` の mock が見つからない）

- [ ] **Step 9: usecase を実装**

`get_news_sentiment.go` の `refresh` で `snap := sentiment.Snapshot{...}` の直後（`if len(articles) > 0 {` の前）に追加:
```go
	if n := len(quotes); n > 0 {
		closePrice := float64(quotes[n-1].Price)
		snap.BasePriceDate = quotes[n-1].Date
		snap.BaseClose = &closePrice
	}
```
touch 経路（`touched := *prev`）は変更不要（コピーで前回の値を保つ）。

- [ ] **Step 10: 全テストが通ることを確認**

Run: `go build ./... && go vet ./... && go test -race -short ./...` と Step 4 の統合テスト
Expected: PASS

- [ ] **Step 11: CLAUDE.md を更新**

`CLAUDE.md` の「ニュースセンチメント」の箇条に、次の1文を追記する（既存の文の末尾）:
`スナップショットにはスコア算出時点の最新終値の日付・値（base_price_date / base_close）も保存し、5営業日後上昇確率の事後検証に使えるようにしている（検証ロジック自体は未実装）。`

- [ ] **Step 12: Commit**

```bash
git add go-api/migrations/005_add_snapshot_base_price.up.sql go-api/migrations/005_add_snapshot_base_price.down.sql \
  go-api/internal/domain/sentiment/sentiment.go go-api/internal/infrastructure/persistence/sentiment_repository.go \
  go-api/internal/infrastructure/persistence/sentiment_repository_test.go \
  go-api/internal/usecase/get_news_sentiment.go go-api/internal/usecase/get_news_sentiment_test.go CLAUDE.md
git commit -m "feat(news-sentiment): store base price date and close on each snapshot"
```

---

### Task 2: 分かりづらい識別子を3つ改名する

動作は変えない。JSON タグ・DB 列名・SQL は変えない。

**Files（参照箇所すべて。`grep -rn` で漏れを確認すること）:**
- `go-api/internal/domain/sentiment/sentiment.go`, `repository.go`, `sentiment_test.go`
- `go-api/internal/infrastructure/persistence/sentiment_repository.go`, `sentiment_repository_test.go`
- `go-api/internal/usecase/get_news_sentiment.go`, `get_news_sentiment_test.go`, `mock_news_sentiment_test.go`
- `go-api/internal/interface/gateway/claude_sentiment_scorer.go`, `claude_sentiment_scorer_test.go`
- `go-api/internal/interface/handler/news_sentiment_handler.go`, `news_sentiment_handler_test.go`

**Interfaces:**
- Produces:
  - `sentiment.Repository.InsertNewArticles(ctx context.Context, articles []Article) error`（旧 `UpsertArticles`。既存の `tdnet_id` 重複は無視して新規分だけ入れる、という実際の挙動に名前を合わせる）
  - `sentiment.StockScores.ShortTermUpProbability int`（旧 `ShortTermUp`）。`Validate` のエラー名は `"short_term_up_probability"`
  - `sentiment.Article.SentimentConfidence *int`（旧 `Confidence`。`ArticleJudgement.Confidence` と `StockScores.Confidence` は**変えない**）

- [ ] **Step 1: `UpsertArticles` → `InsertNewArticles`**

`repository.go` のメソッド名とそのコメント内の名前、`PgSentimentRepository` のメソッド名とエラーメッセージ内の名前があれば同様に、usecase の呼び出し、`MockSentimentRepository` のメソッド名と `m.Called`、テスト内の `On("UpsertArticles", ...)` 文字列、persistence テスト関数名 `TestPgSentimentRepository_UpsertArticles_IgnoresDuplicates` → `TestPgSentimentRepository_InsertNewArticles_IgnoresDuplicates`。
確認: `grep -rn "UpsertArticles" go-api/` が0件。

- [ ] **Step 2: `StockScores.ShortTermUp` → `ShortTermUpProbability`**

フィールド定義・型コメント（`// ShortTermUp は…` → `// ShortTermUpProbability は…`）・`Validate` の `{"short_term_up", s.ShortTermUp}` → `{"short_term_up_probability", s.ShortTermUpProbability}`・`sentiment_test.go` のテーブルキー `"short_term_up"` → `"short_term_up_probability"`、persistence・usecase・gateway・handler とそのテストの全参照。gateway の tool 出力ペイロード構造体のフィールド `ShortTermUp int \`json:"short_term_up_probability"\`` も `ShortTermUpProbability` に揃える（JSON タグはそのまま）。persistence のローカル変数 `shortTermUp` は SQL 列との対応が明らかなので変えなくてよい。
確認: `grep -rnw "ShortTermUp" go-api/` が0件。

- [ ] **Step 3: `Article.Confidence` → `SentimentConfidence`**

`sentiment.go` の `Article` 構造体フィールドを改名し、`go build ./... && go vet ./...`（テスト含めて `go test -race -short -run xxx ./...` でコンパイル）で出るエラー箇所だけを直す。主な箇所: persistence の Scan（`&a.Confidence`）、usecase `scoreUnscoredArticles`（`articles[i].Confidence = &confidence`）、handler のマッピング（`SentimentConfidence: a.Confidence`）、各テストのフィクスチャ。`ArticleJudgement.Confidence` / `StockScores.Confidence` を誤って変えないこと。

- [ ] **Step 4: 検証**

Run: `go build ./... && go vet ./... && go test -race -short ./...` と統合テスト（Global Constraints のコマンド）
Expected: PASS。`git diff --stat` に `migrations/` と frontend が含まれないこと。

- [ ] **Step 5: Commit**

```bash
git add go-api
git commit -m "refactor(news-sentiment): rename InsertNewArticles, ShortTermUpProbability, SentimentConfidence"
```

---

### Task 3: 誤ったコメントの修正と、必要な「なぜ」コメントの追加

動作は変えない（例外: Claude へのプロンプト文言1箇所、項目 13）。各コメントは下記の文面をそのまま使う。行番号は Task 2 適用後にずれるので、記載のコード片で場所を特定すること。

**Files:**
- `go-api/internal/domain/sentiment/sentiment.go`, `repository.go`
- `go-api/internal/usecase/get_news_sentiment.go`
- `go-api/internal/interface/gateway/claude_sentiment_scorer.go`, `tdnet_client.go`
- `go-api/cmd/api/main.go`
- `frontend/app/api/stocks/[code]/news-sentiment/route.ts`

- [ ] **Step 1: domain（sentiment.go / repository.go）**

1. `Fingerprint` の既存2行コメントの後ろに1行追加:
```go
// 注意: スコアラー名やプロンプトは含めないため、SENTIMENT_SCORER を切り替えても入力が同じなら旧スコアラーのスナップショットを使い続ける。
```
2. `PriceContext` の `Return5d` / `Return20d` の行末にコメント: `// %（例: 3.2 は +3.2%）。履歴不足ならnil`（`Return5d` にだけ付ければよい。`Return20d` は同じ単位なので不要）
3. `Snapshot` の `CreatedAt` / `CheckedAt` を次に:
```go
	CreatedAt     time.Time // スコアを算出した時刻（APIでは scored_at）
	CheckedAt     time.Time // 入力が同じか最後に確かめた時刻。キャッシュ期限はこちらで判定する
```
4. `repository.go` の `Scorer.Name()` に: `// Name は scored_by としてDBに保存されるため、固定の識別子を返すこと。`

- [ ] **Step 2: usecase（get_news_sentiment.go）**

5. `const newsSentimentLookbackDays = 90` の上に: `// フロントの「直近90日」表示と合わせること。`
6. `NewsSentiment.Stale` の行末に: `// キャッシュ期限切れで、裏で更新中か更新に失敗した古いスナップショットを返している`
7. `RefreshTimeout` のコメント（3行）を次に置き換え:
```go
	// RefreshTimeout は更新処理全体の上限。最悪ケース（TDnet + 記事判定5チャンク + 銘柄スコア、
	// Claude呼び出しは各45秒×リトライ1回）の合計はこれを超えうるが、超えた分は ctx で打ち切られ、
	// 未判定の記事は次回の更新で再判定される。
```
8. `startRefresh` のコメント2行目の「20秒で待つのを諦めた」→「WaitTimeout で待つのを諦めた」。さらに `u.refreshes.Add(1)` の上に:
```go
	// DoChan より前に Add する: 先に DoChan すると、Wait() が Add より先に戻りうる。
```
`go func() {` の上に:
```go
	// 共有結果を受け取り切ってから Done する中継。呼び出し側が WaitTimeout で諦めても Wait() が更新完了まで待てるようにする。
```
9. `touched := *prev` の上に:
```go
		// prev はリポジトリ（テストではmock）が返した共有ポインタなので、書き換えずにコピーする。
```
10. `Wait()` のコメントを次に: `// Wait は起動済みの更新処理がすべて終わるまで待つ。サーバー停止時、srv.Shutdown の後・DBプールを閉じる前に呼ぶ。最長で RefreshTimeout ブロックする。`

- [ ] **Step 3: gateway**

11. `claude_sentiment_scorer.go` の `ScoreStock` 関数コメント（無ければ関数宣言の直前）に1行:
```go
// 記事ごとの判定ラベルは渡さず、タイトルだけを渡す（設計どおりの意図的な選択。ラベルを渡すのは仕様変更として扱う）。
```
12. `json.Marshal は < > & を …` で始まる文字化けしたコメントを、文字どおり `<` と書いた版に直す。例: `// json.Marshal は < > & を < 等にエスケープするため、…`（`…` 以降は既存の文の後半をそのまま残す）。
13. `formatPriceContext` 内のプロンプト文言「直近30営業日に対するZスコア」→「直前29営業日に対する最新終値のZスコア」。（この文字列を検証するテストは無い。`claude_sentiment_scorer_test.go` の「直近20営業日の騰落率: 不明」は変えない。）
14. `tdnet_client.go` の `tdnetDisclosureLimit = 100` の上に: `// 90日分で100件を超える銘柄は古いものが落ちる（ページングしていない）。`
15. `tdnet_client.go` の `jst` のコメントを次に: `// yanoshinのpubdateはタイムゾーン無しのJST。プロンプトに出す日付もJSTで揃える。コンテナにtzdataが無くても動くよう FixedZone を使う。`

- [ ] **Step 4: main.go と frontend**

16. `go-api/cmd/api/main.go` の `newsSentimentUsecase.Wait()` の上に: `// srv.Shutdown 後（新しい更新が始まらない）かつ pool.Close 前に待つ。最長で RefreshTimeout ブロックする。`
17. `frontend/app/api/stocks/[code]/news-sentiment/route.ts` の `import` の後、既存の `// go-api は…` の上に: `// Server Action ではなく Route Handler にしているのは、Server Action が直列に実行されポーリング中に他の操作を待たせるため。`

- [ ] **Step 5: 検証**

Run（`go-api/`）: `go build ./... && go vet ./... && go test -race -short ./...`
Run（`frontend/`）: `npm test && npx tsc --noEmit && npm run lint`
Expected: すべて PASS。`git diff` が Step 1〜4 の項目以外を含まないこと。

- [ ] **Step 6: Commit**

```bash
git add go-api frontend
git commit -m "docs(news-sentiment): fix misleading comments and explain non-obvious decisions"
```

---

### 完了後

- 統合テストを含む全テストを再実行し、`git push` で PR #27 を更新する（新規PRは作らない）。PR本文のテスト計画に「マイグレーション005適用確認」を追記する。
- メモリ `project_followup_news_sentiment_score_validation.md` を「基準日・基準終値は PR #27 で保存済み。検証ロジックは別PR」に更新する。
