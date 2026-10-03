package main

import (
	"errors"
	"fmt"

	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
)

func newSentimentScorer(kind, anthropicAPIKey, claudeModel string) (sentiment.Scorer, error) {
	switch kind {
	case "", "claude":
		return gateway.NewClaudeSentimentScorer(anthropicAPIKey, claudeModel), nil
	case "jev":
		return nil, errors.New("jev scorer is not implemented yet")
	default:
		return nil, fmt.Errorf("unknown sentiment scorer %q (expected claude)", kind)
	}
}
