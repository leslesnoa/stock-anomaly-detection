package usecase

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const newsSentimentLookbackDays = 90

// articleScoringBatchSize は1回の ScoreArticles 呼び出しに含める未判定記事の上限。
// 開示が多い銘柄でも1リクエストのトークン数・レイテンシを抑え、チャンクごとに
// 成功/失敗を分離できるようにする（失敗したチャンクだけ次回再判定すればよい）。
const articleScoringBatchSize = 20

type NewsSentimentStatus string

const (
	NewsSentimentReady   NewsSentimentStatus = "ready"
	NewsSentimentPending NewsSentimentStatus = "pending"
)

type NewsSentiment struct {
	Status   NewsSentimentStatus
	Stale    bool
	Snapshot *sentiment.Snapshot // pending の時は nil
	Articles []sentiment.Article
}

type NewsSentimentConfig struct {
	CacheTTL time.Duration
	// WaitTimeout はリクエストが更新処理を待つ上限。http.Server.WriteTimeout（30秒）より短くする。
	WaitTimeout time.Duration
	// RefreshTimeout は更新処理全体の上限。TDnet取得、記事判定（最大5チャンク、
	// チャンクごとにClaude呼び出し最悪45秒×リトライ1回）、銘柄スコア取得（同じく
	// 最悪45秒×リトライ1回）を合計で収める。リクエスト自体はそれでも WaitTimeout（20秒）で待つのを諦める。
	RefreshTimeout time.Duration
	Now            func() time.Time
}

func DefaultNewsSentimentConfig() NewsSentimentConfig {
	return NewsSentimentConfig{
		CacheTTL:       6 * time.Hour,
		WaitTimeout:    20 * time.Second,
		RefreshTimeout: 180 * time.Second,
		Now:            time.Now,
	}
}

type GetNewsSentimentUsecase struct {
	watchlists  watchlist.Repository
	prices      stock.PriceRepository
	disclosures news.DisclosureFetcher
	scorer      sentiment.Scorer
	repo        sentiment.Repository
	detector    *anomaly.DetectionService
	cfg         NewsSentimentConfig

	group     singleflight.Group
	refreshes sync.WaitGroup
}

func NewGetNewsSentimentUsecase(
	watchlists watchlist.Repository,
	prices stock.PriceRepository,
	disclosures news.DisclosureFetcher,
	scorer sentiment.Scorer,
	repo sentiment.Repository,
	detector *anomaly.DetectionService,
	cfg NewsSentimentConfig,
) *GetNewsSentimentUsecase {
	return &GetNewsSentimentUsecase{
		watchlists:  watchlists,
		prices:      prices,
		disclosures: disclosures,
		scorer:      scorer,
		repo:        repo,
		detector:    detector,
		cfg:         cfg,
	}
}

// Handle はキャッシュが新しければそれを返し、古ければ更新処理を起動して WaitTimeout まで待つ。
// 待ちきれない・失敗した場合は古いスナップショット（stale）か pending を返し、エラーにはしない。
func (u *GetNewsSentimentUsecase) Handle(ctx context.Context, userID, rawStockCode string) (NewsSentiment, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return NewsSentiment{}, err
	}
	if err := ensureInWatchlist(ctx, u.watchlists, userID, code); err != nil {
		return NewsSentiment{}, err
	}

	latest, err := u.repo.FindLatestSnapshot(ctx, code.String())
	if err != nil {
		return NewsSentiment{}, err
	}
	if latest != nil && u.cfg.Now().Sub(latest.CheckedAt) < u.cfg.CacheTTL {
		return u.ready(ctx, code, latest, false)
	}

	result := u.startRefresh(code)
	timer := time.NewTimer(u.cfg.WaitTimeout)
	defer timer.Stop()
	select {
	case r := <-result:
		if r.Err == nil {
			return u.ready(ctx, code, r.Val.(*sentiment.Snapshot), false)
		}
		log.Printf("WARN news sentiment refresh failed for %s: %v", code, r.Err)
	case <-timer.C:
		log.Printf("WARN news sentiment refresh for %s exceeded %s; continuing in background", code, u.cfg.WaitTimeout)
	case <-ctx.Done():
		return NewsSentiment{}, ctx.Err()
	}

	if latest != nil {
		return u.ready(ctx, code, latest, true)
	}
	return NewsSentiment{Status: NewsSentimentPending, Articles: []sentiment.Article{}}, nil
}

// Wait は起動済みの更新処理がすべて終わるまで待つ。サーバー停止時に、DBプールを閉じる前に呼ぶ。
func (u *GetNewsSentimentUsecase) Wait() {
	u.refreshes.Wait()
}

// startRefresh は同じ銘柄の更新を singleflight で1本にまとめる。更新処理はリクエストの
// context から切り離して最後まで走らせ、20秒で待つのを諦めたリクエストの分も結果をDBに残す。
func (u *GetNewsSentimentUsecase) startRefresh(code stock.StockCode) <-chan singleflight.Result {
	u.refreshes.Add(1)
	shared := u.group.DoChan(code.String(), func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), u.cfg.RefreshTimeout)
		defer cancel()
		return u.refresh(ctx, code)
	})
	out := make(chan singleflight.Result, 1)
	go func() {
		defer u.refreshes.Done()
		out <- <-shared
	}()
	return out
}

func (u *GetNewsSentimentUsecase) ready(ctx context.Context, code stock.StockCode, snap *sentiment.Snapshot, stale bool) (NewsSentiment, error) {
	articles, err := u.repo.FindArticlesSince(ctx, code.String(), u.since())
	if err != nil {
		return NewsSentiment{}, err
	}
	return NewsSentiment{Status: NewsSentimentReady, Stale: stale, Snapshot: snap, Articles: articles}, nil
}

func (u *GetNewsSentimentUsecase) since() time.Time {
	return u.cfg.Now().AddDate(0, 0, -newsSentimentLookbackDays)
}

func (u *GetNewsSentimentUsecase) refresh(ctx context.Context, code stock.StockCode) (*sentiment.Snapshot, error) {
	now := u.cfg.Now()
	since := u.since()

	disclosures, err := u.disclosures.FetchDisclosures(ctx, code, since)
	if err != nil {
		return nil, fmt.Errorf("fetch disclosures: %w", err)
	}
	fetched := make([]sentiment.Article, len(disclosures))
	for i, d := range disclosures {
		fetched[i] = sentiment.Article{StockCode: code.String(), TdnetID: d.TdnetID, Title: d.Title, URL: d.URL, PublishedAt: d.PublishedAt}
	}
	if err := u.repo.UpsertArticles(ctx, fetched); err != nil {
		return nil, fmt.Errorf("save articles: %w", err)
	}

	articles, err := u.repo.FindArticlesSince(ctx, code.String(), since)
	if err != nil {
		return nil, fmt.Errorf("load articles: %w", err)
	}
	u.scoreUnscoredArticles(ctx, code, articles, now)

	quotes, err := u.prices.FindRecent(ctx, code, historySize)
	if err != nil {
		return nil, fmt.Errorf("load prices: %w", err)
	}
	price := buildPriceContext(quotes, u.detector)
	fingerprint := sentiment.Fingerprint(articles, price.LatestDate)

	prev, err := u.repo.FindLatestSnapshot(ctx, code.String())
	if err != nil {
		return nil, fmt.Errorf("load previous snapshot: %w", err)
	}
	if prev != nil && prev.InputFingerprint == fingerprint {
		if err := u.repo.TouchSnapshot(ctx, prev.ID, now); err != nil {
			return nil, fmt.Errorf("touch snapshot: %w", err)
		}
		touched := *prev
		touched.CheckedAt = now
		return &touched, nil
	}

	snap := sentiment.Snapshot{
		StockCode:        code.String(),
		ArticleCount:     len(articles),
		InputFingerprint: fingerprint,
		ScoredBy:         u.scorer.Name(),
		CreatedAt:        now,
		CheckedAt:        now,
	}
	if n := len(quotes); n > 0 {
		closePrice := float64(quotes[n-1].Price)
		snap.BasePriceDate = quotes[n-1].Date
		snap.BaseClose = &closePrice
	}
	if len(articles) > 0 {
		scores, err := u.scorer.ScoreStock(ctx, articles, price)
		if err != nil {
			return nil, fmt.Errorf("score stock: %w", err)
		}
		snap.Scores = &scores
	}
	id, err := u.repo.InsertSnapshot(ctx, snap)
	if err != nil {
		return nil, fmt.Errorf("save snapshot: %w", err)
	}
	snap.ID = id
	return &snap, nil
}

// scoreUnscoredArticles は未判定の記事だけを、既存の並び順（新しい順）を保ったまま
// articleScoringBatchSize 件ずつのチャンクに分けて判定し、articles をその場で更新する。
// チャンクは独立に処理する: あるチャンクの ScoreArticles や SaveJudgements が失敗しても
// WARN を残して次のチャンクへ進み、銘柄スコアの計算は続ける（保存できなかったチャンクの
// 記事は未判定のままなので、次回の更新で再判定される）。
func (u *GetNewsSentimentUsecase) scoreUnscoredArticles(ctx context.Context, code stock.StockCode, articles []sentiment.Article, now time.Time) {
	var indexes []int
	for i, a := range articles {
		if a.Sentiment == nil {
			indexes = append(indexes, i)
		}
	}
	if len(indexes) == 0 {
		return
	}
	scoredBy := u.scorer.Name()

	for start := 0; start < len(indexes); start += articleScoringBatchSize {
		end := start + articleScoringBatchSize
		if end > len(indexes) {
			end = len(indexes)
		}
		chunk := indexes[start:end]

		targets := make([]sentiment.Article, len(chunk))
		ids := make([]string, len(chunk))
		for k, i := range chunk {
			targets[k] = articles[i]
			ids[k] = articles[i].ID
		}

		judgements, err := u.scorer.ScoreArticles(ctx, targets)
		if err != nil {
			log.Printf("WARN article sentiment scoring failed for %s (articles %d-%d): %v", code, start, end, err)
			continue
		}
		if err := u.repo.SaveJudgements(ctx, ids, judgements, scoredBy, now); err != nil {
			log.Printf("WARN save article judgements failed for %s (articles %d-%d): %v", code, start, end, err)
			continue
		}
		for k, i := range chunk {
			label, confidence, at := judgements[k].Sentiment, judgements[k].Confidence, now
			articles[i].Sentiment = &label
			articles[i].Confidence = &confidence
			articles[i].ScoredBy = &scoredBy
			articles[i].ScoredAt = &at
		}
	}
}

// buildPriceContext は古い順の終値から、AIに渡す株価の状況を作る。履歴が足りない指標は nil にする。
func buildPriceContext(quotes []stock.Quote, detector *anomaly.DetectionService) sentiment.PriceContext {
	pc := sentiment.PriceContext{}
	n := len(quotes)
	if n == 0 {
		return pc
	}
	pc.LatestDate = quotes[n-1].Date
	pc.Return5d = percentChange(quotes, 5)
	pc.Return20d = percentChange(quotes, 20)
	if n >= historySize {
		window := make([]float64, historySize)
		for i, q := range quotes[n-historySize:] {
			window[i] = float64(q.Price)
		}
		if z, err := detector.Calculate(window); err == nil {
			zf := float64(z)
			pc.ZScore = &zf
		}
	}
	return pc
}

func percentChange(quotes []stock.Quote, days int) *float64 {
	n := len(quotes)
	if n <= days {
		return nil
	}
	base := float64(quotes[n-1-days].Price)
	if base == 0 {
		return nil
	}
	v := (float64(quotes[n-1].Price)/base - 1) * 100
	return &v
}
