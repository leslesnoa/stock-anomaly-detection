package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePollTime(t *testing.T) {
	h, m, err := parsePollTime("16:00")
	require.NoError(t, err)
	assert.Equal(t, 16, h)
	assert.Equal(t, 0, m)

	h, m, err = parsePollTime("09:30")
	require.NoError(t, err)
	assert.Equal(t, 9, h)
	assert.Equal(t, 30, m)
}

func TestParsePollTime_Invalid(t *testing.T) {
	for _, s := range []string{"", "16", "16:60", "24:00", "aa:bb", "16:0:0", "-1:00"} {
		_, _, err := parsePollTime(s)
		assert.Error(t, err, "input %q should be rejected", s)
	}
}
