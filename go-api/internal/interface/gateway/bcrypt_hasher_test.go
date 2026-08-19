package gateway_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBcryptHasher_HashAndVerify(t *testing.T) {
	h := gateway.NewBcryptHasher()

	hash, err := h.Hash("correct-password")
	require.NoError(t, err)
	assert.NotEqual(t, "correct-password", hash)

	assert.NoError(t, h.Verify(hash, "correct-password"))
	assert.Error(t, h.Verify(hash, "wrong-password"))
}
