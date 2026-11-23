package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/luizflm/payment-processor/internal/platform/config"
)

// New returns a JSON logger tagged with the service name, writing to
// stdout. The level comes from LOG_LEVEL (debug, info, warn, error),
// defaulting to info when unset or invalid.
func New(service string) *slog.Logger {
	return NewWithWriter(service, os.Stdout)
}

// NewWithWriter returns a JSON logger tagged with the service name, writing to w.
func NewWithWriter(service string, w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     levelFromEnv(),
		AddSource: true,
	})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(config.String("LOG_LEVEL", "")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
