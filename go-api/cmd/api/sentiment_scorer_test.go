package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSentimentScorer(t *testing.T) {
	for _, kind := range []string{"", "claude"} {
		scorer, err := newSentimentScorer(kind, "key", "claude-opus-5")
		require.NoError(t, err, kind)
		assert.Equal(t, "claude", scorer.Name())
	}

	_, err := newSentimentScorer("jev", "key", "claude-opus-5")
	require.ErrorContains(t, err, "not implemented")

	_, err = newSentimentScorer("gpt", "key", "claude-opus-5")
	require.ErrorContains(t, err, "unknown")
}
