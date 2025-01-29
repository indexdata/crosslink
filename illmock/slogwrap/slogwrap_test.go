package slogwrap

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmpty(t *testing.T) {
	logger := slogEnable("")
	assert.Equal(t, logger, slog.Default())
}

func TestFalse(t *testing.T) {
	logger := slogEnable("false")
	assert.Equal(t, logger, slog.Default())
}

func TestTrue(t *testing.T) {
	logger := slogEnable("true")
	assert.NotEqual(t, logger, slog.Default())
}
