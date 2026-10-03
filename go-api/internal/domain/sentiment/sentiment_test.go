package sentiment_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLabel(t *testing.T) {
	for _, s := range []string{"bullish", "bearish", "neutral"} {
		got, err := sentiment.ParseLabel(s)
		require.NoError(t, err)
		assert.Equal(t, sentiment.Label(s), got)
	}
	_, err := sentiment.ParseLabel("positive")
	require.Error(t, err)
}

func TestArticleJudgement_Validate(t *testing.T) {
	require.NoError(t, sentiment.ArticleJudgement{Sentiment: sentiment.Bullish, Confidence: 0}.Validate())
	require.NoError(t, sentiment.ArticleJudgement{Sentiment: sentiment.Neutral, Confidence: 100}.Validate())
	require.ErrorIs(t, sentiment.ArticleJudgement{Sentiment: sentiment.Bearish, Confidence: 101}.Validate(), sentiment.ErrInvalidScore)
	require.ErrorIs(t, sentiment.ArticleJudgement{Sentiment: sentiment.Bearish, Confidence: -1}.Validate(), sentiment.ErrInvalidScore)
	require.Error(t, sentiment.ArticleJudgement{Sentiment: "up", Confidence: 50}.Validate())
}

func TestStockScores_Validate(t *testing.T) {
	valid := sentiment.StockScores{Bullish: 72, Bearish: 18, Impact: 55, Confidence: 40, ShortTermUpProbability: 58}
	require.NoError(t, valid.Validate())

	for name, mutate := range map[string]func(*sentiment.StockScores){
		"bullish":                   func(s *sentiment.StockScores) { s.Bullish = 101 },
		"bearish":                   func(s *sentiment.StockScores) { s.Bearish = -1 },
		"impact":                    func(s *sentiment.StockScores) { s.Impact = 200 },
		"confidence":                func(s *sentiment.StockScores) { s.Confidence = -5 },
		"short_term_up_probability": func(s *sentiment.StockScores) { s.ShortTermUpProbability = 101 },
	} {
		t.Run(name, func(t *testing.T) {
			s := valid
			mutate(&s)
			require.ErrorIs(t, s.Validate(), sentiment.ErrInvalidScore)
		})
	}
}

func TestFingerprint(t *testing.T) {
	a := []sentiment.Article{{TdnetID: "1"}, {TdnetID: "2"}}
	b := []sentiment.Article{{TdnetID: "2"}, {TdnetID: "1"}}

	assert.Equal(t, sentiment.Fingerprint(a, "2026-09-29"), sentiment.Fingerprint(b, "2026-09-29"), "記事の並び順に依存しない")
	assert.NotEqual(t, sentiment.Fingerprint(a, "2026-09-29"), sentiment.Fingerprint(a, "2026-09-30"), "最新株価日付が変われば変わる")
	assert.NotEqual(t, sentiment.Fingerprint(a, "2026-09-29"), sentiment.Fingerprint(a[:1], "2026-09-29"), "記事集合が変われば変わる")
	assert.NotEqual(t, sentiment.Fingerprint(nil, ""), "", "空入力でも空文字にしない")
}
