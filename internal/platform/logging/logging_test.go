package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/luizflm/payment-processor/internal/platform/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	logger := logging.New("test-service")
	require.NotNil(t, logger)
	assert.Same(t, logger, slog.Default())
}

func TestNewWithWriter(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  string
		wantLevel slog.Level
	}{
		{name: "default", wantLevel: slog.LevelInfo},
		{name: "invalid", logLevel: "invalid", wantLevel: slog.LevelInfo},
		{name: "debug", logLevel: "debug", wantLevel: slog.LevelDebug},
		{name: "info", logLevel: "info", wantLevel: slog.LevelInfo},
		{name: "warn", logLevel: "warn", wantLevel: slog.LevelWarn},
		{name: "error", logLevel: "error", wantLevel: slog.LevelError},
		{name: "uppercase", logLevel: "DEBUG", wantLevel: slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := slog.Default()
			t.Cleanup(func() { slog.SetDefault(previous) })
			t.Setenv("LOG_LEVEL", tt.logLevel)

			var buf bytes.Buffer
			logger := logging.NewWithWriter("test-service", &buf)
			require.NotNil(t, logger)
			assert.Same(t, logger, slog.Default())
			assert.False(t, logger.Enabled(t.Context(), tt.wantLevel-1))
			logger.Log(t.Context(), tt.wantLevel, "hello")

			var record map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
			assert.Equal(t, "hello", record["msg"])
			assert.Equal(t, "test-service", record["service"])
			assert.Equal(t, tt.wantLevel.String(), record["level"])
		})
	}
}
