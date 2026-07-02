// Package logging provides a small wrapper around log/slog so that every
// component in Brevitas shares one structured logger with consistent
// configuration (level, format, output).
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Format selects the encoding used by the logger.
type Format string

const (
	// FormatText emits human-readable key=value lines (default for the CLI).
	FormatText Format = "text"
	// FormatJSON emits one JSON object per line (default for the service).
	FormatJSON Format = "json"
)

// Options configures the process-wide logger.
type Options struct {
	Level  slog.Level
	Format Format
	Output io.Writer
}

var (
	mu      sync.RWMutex
	current = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
)

// Init configures the process-wide logger and returns it.
func Init(opts Options) *slog.Logger {
	if opts.Output == nil {
		opts.Output = os.Stderr
	}
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var handler slog.Handler
	switch opts.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(opts.Output, handlerOpts)
	default:
		handler = slog.NewTextHandler(opts.Output, handlerOpts)
	}

	logger := slog.New(handler)

	mu.Lock()
	current = logger
	mu.Unlock()

	slog.SetDefault(logger)
	return logger
}

// L returns the current process-wide logger.
func L() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// FromContext returns a logger, preferring one stored on the context.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return L()
}

// WithContext stores a logger on the context for later retrieval.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

type loggerKey struct{}

// ParseLevel converts a string such as "debug" into an slog.Level. Unknown
// values fall back to Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
