# ニュースセンチメント表示 設計

## 目的

銘柄詳細ページ（`frontend/app/stocks/[code]/page.tsx`）の株価チャートの下に、その銘柄の直近のTDnet開示（ニュース）を表示する。各記事には強気（勝ち気）/弱気（負け気）/中立のバッジを付け、ニュース全体と株価の状況を踏まえてAIが推測したスコアを銘柄単位で表示する。

ユーザーがチャートを見ながら「最近どんな材料が出ていて、それは良い材料か悪い材料か」を一目で把握できることが成功基準。

## 現状

- TDnetニュースは `gateway.YanoshinTDnetClient.FetchRecent` で取得している（直近7日・最大5件・タイトルのみ）。使われているのはSlack異常通知フロー（`AnalyzeAndNotifyUsecase`）の中だけで、DBへの保存やフロント向けのAPIは無い。
- ニュースのセンチメントを判定するロジックは存在しない。
- 詳細ページはチャートのCardのみで、その下には何も表示していない。
- yanoshin TDnet APIのレスポンスには、安定した `id`、`pubdate`（タイムゾーン無しのJST、`2006-01-02 15:04:05`）、`title`、`document_url`（PDFへのリダイレクトURL）が含まれる。`limit=100` で主要銘柄でも約2年以上の開示が返る。
- 実測では、トヨタ（7203）でも30日以内の開示は1件だけだった。

## 決定事項

| 項目 | 決定 |
|---|---|
| 構成 | 専用エンドポイント `GET /stocks/{code}/news-sentiment` を新設する。フロントはチャートとは別に非同期で取得する |
| 対象期間 | 直近90日のTDnet開示 |
| 表示件数 | 新しい順に最大5件。残りは「もっと見る」で展開する |
| 記事判定 | タイトルのみを材料に `bullish` / `bearish` / `neutral` と確率（0〜100）を出す。一度判定した記事は再計算しない |
| 銘柄スコア | 勝ち気・負け気・インパクト度・確信度・5営業日後上昇確率（各0〜100、互いに独立で合計100にはならない）。材料は90日分の記事タイトル（公開日付き）と株価の状況 |
| キャッシュ | 銘柄単位で全ユーザー共有。6時間。`singleflight` で同時リクエストの分析を1回にまとめる。入力が変わっていなければAIを呼ばない |
| 時間の上限 | リクエストは20秒で打ち切る。分析自体はバックグラウンドで最後まで走らせて保存する |
| 判定器 | `sentiment.Scorer` インターフェース。今回はClaude実装のみ。`SENTIMENT_SCORER` 環境変数で切り替える |
| Slack通知 | 今回は変更しない |

## アーキテクチャ

依存方向は既存と同じく `interface(handler/gateway/persistence)` → `usecase` → `domain`。

```
frontend NewsSentimentPanel
   │ GET /stocks/{code}/news-sentiment
   ▼
handler.NewsSentimentHandler
   ▼
usecase.GetNewsSentimentUsecase ──┬─ watchlist.Repository（登録確認）
                                  ├─ news.DisclosureFetcher（TDnet 90日）
                                  ├─ stock.PriceRepository（株価の状況）
                                  ├─ sentiment.Scorer（Claude / 将来Jev）
                                  └─ sentiment.Repository（記事・スナップショット）
```

### ドメイン層

`internal/domain/news` に、既存の `Item`/`Fetcher` とは別に、90日分をURL・日時付きで取得するための型を追加する。既存の `Fetcher.FetchRecent` はSlack通知フローで使っているので挙動を変えない。

```go
type Disclosure struct {
    TdnetID     string
    Title       string
    URL         string
    PublishedAt time.Time // JST
}

type DisclosureFetcher interface {
    FetchDisclosures(ctx context.Context, code stock.StockCode, since time.Time) ([]Disclosure, error)
}
```

`internal/domain/sentiment` を新設する。

```go
type Label string // "bullish" | "bearish" | "neutral"

type Article struct {
    ID          string // news_articles.id
    StockCode   string
    TdnetID     string
    Title       string
    URL         string
    PublishedAt time.Time
    Sentiment   *Label // 未判定ならnil
    Confidence  *int
    ScoredBy    *string
    ScoredAt    *time.Time
}

type ArticleJudgement struct {
    Sentiment  Label
    Confidence int // 0-100
}

type PriceContext struct {
    LatestDate string   // 最新終値の日付
    Return5d   *float64 // 直近5営業日の騰落率。履歴不足ならnil
    Return20d  *float64
    ZScore     *float64
}

type StockScores struct {
    Bullish     int
    Bearish     int
    Impact      int
    Confidence  int
    ShortTermUp int // 5営業日後の終値が最新終値を上回る確率
}

type Snapshot struct {
    ID               string
    StockCode        string
    Scores           *StockScores // 対象記事0件ならnil
    ArticleCount     int
    InputFingerprint string
    ScoredBy         string
    CreatedAt        time.Time
    CheckedAt        time.Time
}

type Scorer interface {
    Name() string
    ScoreArticles(ctx context.Context, articles []Article) ([]ArticleJudgement, error) // 入力と同じ順・同じ件数
    ScoreStock(ctx context.Context, articles []Article, price PriceContext) (StockScores, error)
}

type Repository interface {
    UpsertArticles(ctx context.Context, articles []Article) error // (stock_code, tdnet_id) の衝突は無視
    SaveJudgements(ctx context.Context, articleIDs []string, js []ArticleJudgement, scoredBy string, at time.Time) error
    FindArticlesSince(ctx context.Context, code string, since time.Time) ([]Article, error) // 公開日の降順
    FindLatestSnapshot(ctx context.Context, code string) (*Snapshot, error) // 無ければnil
    InsertSnapshot(ctx context.Context, s Snapshot) error
    TouchSnapshot(ctx context.Context, id string, checkedAt time.Time) error
}
```

### DBスキーマ（`004_create_news_sentiment.up.sql` / `.down.sql`）

```sql
CREATE TABLE IF NOT EXISTS news_articles (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stock_code           TEXT NOT NULL,
    tdnet_id             TEXT NOT NULL,
    title                TEXT NOT NULL,
    url                  TEXT NOT NULL,
    published_at         TIMESTAMPTZ NOT NULL,
    sentiment            TEXT CHECK (sentiment IN ('bullish', 'bearish', 'neutral')),
    sentiment_confidence SMALLINT CHECK (sentiment_confidence BETWEEN 0 AND 100),
    scored_by            TEXT,
    scored_at            TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stock_code, tdnet_id)
);
CREATE INDEX IF NOT EXISTS news_articles_stock_published_idx
    ON news_articles (stock_code, published_at DESC);

CREATE TABLE IF NOT EXISTS stock_sentiment_snapshots (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stock_code                TEXT NOT NULL,
    bullish_score             SMALLINT CHECK (bullish_score BETWEEN 0 AND 100),
    bearish_score             SMALLINT CHECK (bearish_score BETWEEN 0 AND 100),
    impact_score              SMALLINT CHECK (impact_score BETWEEN 0 AND 100),
    confidence_score          SMALLINT CHECK (confidence_score BETWEEN 0 AND 100),
    short_term_up_probability SMALLINT CHECK (short_term_up_probability BETWEEN 0 AND 100),
    article_count             INTEGER NOT NULL,
    input_fingerprint         TEXT NOT NULL,
    scored_by                 TEXT NOT NULL,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS stock_sentiment_snapshots_stock_created_idx
    ON stock_sentiment_snapshots (stock_code, created_at DESC);
```

- スコア列は、対象記事が0件のスナップショットではすべてNULLになる。
- スナップショットは上書きせずに履歴として積み上げる。後で「5営業日後上昇確率」の的中率を検証できるようにするため。
- `.down.sql` は `DROP TABLE IF EXISTS` で冪等にする（`docker-compose` の初期化時に `.down.sql` が先に実行されるため）。

### ユースケース `GetNewsSentimentUsecase.Handle(ctx, userID, code)`

1. 証券コードを検証し、ユーザーのwatchlistに登録されているかを確認する（未登録なら `watchlist.ErrNotFound` で404。既存の `/chart` と同じルール）。
2. 最新スナップショットの `checked_at` が6時間以内なら、スナップショットと90日分の記事をDBから返す（`status: ready`, `stale: false`）。
3. 6時間を過ぎているか、スナップショットが無い場合は、`singleflight`（キーは証券コード）で次の「更新処理」を起動する。更新処理はリクエストのcontextから切り離したcontext（上限60秒）で動かす。
   1. TDnetから直近90日分を取得し、`UpsertArticles` で保存する。
   2. 未判定の記事だけを `Scorer.ScoreArticles` で判定し、`SaveJudgements` で保存する。
   3. 90日分の記事と株価の状況（`PriceRepository.FindRecent` から計算した5日/20日騰落率、Zスコア、最新日付）から入力の指紋を作る。指紋は「ソートした記事の `tdnet_id` 群＋最新株価日付」のSHA-256。
   4. 指紋が前回のスナップショットと同じなら、`TouchSnapshot` で `checked_at` だけを更新する（AIは呼ばない）。
   5. 違っていれば、`Scorer.ScoreStock` で銘柄スコアを出し、`InsertSnapshot` で追加する。記事が0件ならAIを呼ばず、スコアがNULLのスナップショットを追加する。
4. リクエスト側は更新処理の完了を最大20秒待つ。
   - 完了した → 最新の結果を `status: ready` で返す。
   - 失敗した、または20秒を超えた → 古いスナップショットがあれば `status: ready, stale: true` で返す。無ければ `status: pending` で返す（更新処理はバックグラウンドで続行する）。
   - 失敗した、かつ古いスナップショットが無い → `status: pending` とし、更新処理のエラーはログに出す。次のリクエストで再試行される。

記事判定（手順3-2）が失敗した場合は、そのまま銘柄スコアの計算に進む（未判定の記事はバッジ無しで表示し、次回の更新で再判定する）。

### 判定器（`gateway.ClaudeSentimentScorer`）

- 既存の `ANTHROPIC_API_KEY` と `CLAUDE_MODEL` を使う。
- Claudeのtool use（入力スキーマ付きのツール定義）で構造化JSONを出させる。自由文はパースしない。
- `ScoreArticles` は全記事を1リクエストにまとめる。レスポンスの件数が入力と違う、または値が0〜100の範囲外ならエラーにする。
- `ScoreStock` のプロンプトには、各記事の公開日とタイトル、株価の状況を渡す。そのうえで次を指示する。
  - 古い材料ほど重みを下げる
  - 判断材料が少ない・曖昧な時は確信度を下げる
  - 短期上昇確率は「5営業日後の終値が、最新終値を上回る確率」と定義する
- タイトルは開示企業が書いた文字列なので、プロンプト内ではデータとして区切って渡す。出力は数値とラベルのみで、範囲の検証をするため、仮にタイトルに指示文が混ざっていても影響は数値の範囲内にとどまる。
- 失敗時は1回リトライする（既存のClaude呼び出しと同じ方針）。

`main.go` の切り替え:
- `SENTIMENT_SCORER` が未設定または `claude` → `ClaudeSentimentScorer`
- `jev` → 今回は未実装なので起動時にエラーで止める（Jev実装は別PR）
- それ以外 → 起動時にエラー

### TDnetクライアントの拡張

`YanoshinTDnetClient` に `FetchDisclosures(ctx, code, since)` を追加する。`limit=100` で取得し、`pubdate` をJSTとして解釈して `since` より前のものを除外する。`document_url` は、yanoshinのリダイレクタURLのまま保存・表示する。`pubdate` を解釈できない記事は捨ててログに出す。

### API

`GET /stocks/{code}/news-sentiment`（`RequireAuth` 必須）

```json
{
  "status": "ready",
  "stale": false,
  "scores": {
    "bullish": 72, "bearish": 18, "impact": 55, "confidence": 40,
    "short_term_up_probability": 58
  },
  "scored_by": "claude",
  "scored_at": "2026-09-30T15:10:00+09:00",
  "articles": [
    {
      "title": "...", "url": "...", "published_at": "2026-09-03T15:30:00+09:00",
      "sentiment": "bullish", "sentiment_confidence": 81
    }
  ]
}
```

- `status: "pending"` の時は、`scores`/`scored_by`/`scored_at` は `null`、`articles` は空配列。
- 記事が0件のスナップショットの時は、`scores` は `null`、`articles` は空配列。
- 未判定の記事は、`sentiment`/`sentiment_confidence` が `null`。
- エラー: 不正な証券コードは400、watchlist未登録は404、その他は500。

### フロントエンド

`frontend/components/news-sentiment-panel.tsx` を新規作成し、`page.tsx` のチャートCardの下に置く。データは `lib/go-api-client` に追加する `fetchNewsSentiment` で取得する。

- **スコア欄**: 5種類のスコアをバーで表示する。勝ち気は緑、負け気は赤、その他はニュートラル色。短期上昇確率は「ニュースAI推測: 5営業日後に上昇している確率」と表示し、既存の方向分類器（統計モデル）のバッジと出典を区別する。
- **記事リスト**: 公開日、強気/弱気/中立のバッジ、タイトル（`url` へのリンク、新しいタブで開く）。最初は5件で、6件以上あれば「もっと見る」で全件を展開する。未判定の記事はバッジを出さない。
- **状態ごとの表示**
  - 読み込み中・`pending` → 「AI分析中…」。`pending` の時は10秒間隔で最大3回まで自動で再取得し、それでも `pending` なら「時間がかかっています。後で再表示してください」
  - `stale: true` → 「最新の分析ではありません（{scored_at}時点）」
  - `scores: null` かつ記事0件 → 「直近90日に判断材料となる開示がありません」
  - 取得エラー → このパネルだけエラー表示にする（チャートには影響させない）
- **注意書き**を常に表示する:「ニュースタイトルと株価推移に基づくAIの推測です。投資助言ではありません。」

## テスト

- **usecase**（モックで確認）
  - 6時間以内のキャッシュヒット時に、TDnetとScorerが呼ばれない
  - 6時間を過ぎていて指紋が同じ時に、`ScoreStock` が呼ばれず `TouchSnapshot` だけが呼ばれる
  - 指紋が違う時に、未判定の記事だけが `ScoreArticles` に渡され、スナップショットが追加される
  - 記事が0件の時に、Scorerが呼ばれずスコアNULLのスナップショットになる
  - Scorerが失敗し、古いスナップショットがある時に `stale: true` になる
  - 古いスナップショットが無く、20秒を超えた時に `pending` になる（テストでは締め切りを注入して短くする）
  - 同時リクエストで更新処理が1回にまとまる（`-race` 付き）
  - watchlist未登録で `ErrNotFound`
- **gateway**（`httptest` でモック）
  - TDnet: 90日での絞り込み、JSTでの解釈、URL・IDの取り出し、不正な `pubdate` の除外
  - Claude: tool useレスポンスのパース、件数不一致・範囲外の値でのエラー、リトライ
- **persistence**（統合テスト。`DATABASE_URL` 未設定または `-short` の時はスキップ）
  - `(stock_code, tdnet_id)` の重複が無視される
  - 最新スナップショットの取得、`checked_at` の更新
- **handler**: レスポンスの形（`pending`、`scores: null`、未判定記事の `null`）、400/404
- **frontend（Vitest）**: 各状態の表示、5件＋「もっと見る」、`pending` での自動再取得、注意書きの常時表示

## 非スコープ・既知の制約

- **Jev実装**: TypeSafe AIのAPIキー取得後に別PRで追加する。実APIで仕様を確認し、日本語タイトルでの精度をClaude版と比べる。公式ドキュメントには「CJK言語の精度は英語と同等ではない」とある。
- **Slack通知への組み込み**: Jevの精度を検証するまで行わない。
- **開示PDF本文の利用**: 今回はタイトルのみ。「業績予想の修正」のように、タイトルだけでは上方か下方か分からない開示は `neutral` に寄りやすい。
- **データの削除**: 記事は銘柄ごとにゆっくり増えるだけで、スナップショットも入力が変わった時だけ追加されるので、削除処理は作らない。
- **呼び出し量**: AI呼び出しは「watchlistの銘柄数 × 最大6時間に1回」に抑えられる。ただし、watchlistの登録数に上限が無い問題（既存のフォローアップ）はそのまま残る。
- **スコアの精度検証**: スナップショットの履歴は残すが、的中率を計算・表示する仕組みは今回作らない。

## 環境変数

- `SENTIMENT_SCORER`（任意、デフォルト `claude`）。`claude` のみ有効。`jev` は別PRで追加する。
- 既存の `ANTHROPIC_API_KEY`、`CLAUDE_MODEL` を流用する。
