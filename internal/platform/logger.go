package platform

import (
	"log/slog"
	"os"
)

// NewLogger returns the JSON logger used by the service.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
