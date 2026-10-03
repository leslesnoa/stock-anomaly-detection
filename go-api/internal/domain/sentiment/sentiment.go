package sentiment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Label string

const (
	Bullish Label = "bullish"
	Bearish Label = "bearish"
	Neutral Label = "neutral"
)

var ErrInvalidScore = errors.New("sentiment score out of range")

func ParseLabel(s string) (Label, error) {
	switch Label(s) {
	case Bullish, Bearish, Neutral:
		return Label(s), nil
	}
	return "", fmt.Errorf("invalid sentiment label %q", s)
}

type Article struct {
	ID          string
	StockCode   string
	TdnetID     string
	Title       string
	URL         string
	PublishedAt time.Time
	Sentiment   *Label
	Confidence  *int
	ScoredBy    *string
	ScoredAt    *time.Time
}

type ArticleJudgement struct {
	Sentiment  Label
	Confidence int
}

func (j ArticleJudgement) Validate() error {
	if _, err := ParseLabel(string(j.Sentiment)); err != nil {
		return err
	}
	return validatePercent("confidence", j.Confidence)
}

type PriceContext struct {
	LatestDate string
	Return5d   *float64
	Return20d  *float64
	ZScore     *float64
}

// StockScores の各値は0-100で互いに独立（合計100にはならない）。
// ShortTermUp は「5営業日後の終値が、スコア算出時点の最新終値を上回る確率」。
type StockScores struct {
	Bullish     int
	Bearish     int
	Impact      int
	Confidence  int
	ShortTermUp int
}

func (s StockScores) Validate() error {
	for _, f := range []struct {
		name  string
		value int
	}{
		{"bullish", s.Bullish},
		{"bearish", s.Bearish},
		{"impact", s.Impact},
		{"confidence", s.Confidence},
		{"short_term_up", s.ShortTermUp},
	} {
		if err := validatePercent(f.name, f.value); err != nil {
			return err
		}
	}
	return nil
}

func validatePercent(name string, v int) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("%w: %s=%d", ErrInvalidScore, name, v)
	}
	return nil
}

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

// Fingerprint は銘柄スコアの入力（記事集合と最新株価日付）が前回から変わったかを判定するための値。
// 入力が同じなのにLLMを呼び直すと、スコアだけが揺れてユーザーに「何か起きた」ように見えるため。
func Fingerprint(articles []Article, latestPriceDate string) string {
	ids := make([]string, len(articles))
	for i, a := range articles {
		ids[i] = a.TdnetID
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",") + "|" + latestPriceDate))
	return hex.EncodeToString(sum[:])
}
