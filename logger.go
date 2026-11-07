package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const (
	levelTrace = slog.Level(-8)
	levelCrit  = slog.Level(12)
)

func newComponentLogger(component string) (*slog.Logger, slog.Level, error) {
	level, err := parseLogLevel(os.Getenv("MCP_TASKFILE_LOG_LEVEL"))
	if err != nil {
		return nil, 0, err
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: level <= slog.LevelDebug,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.LevelKey {
				if lvl, ok := attr.Value.Any().(slog.Level); ok {
					attr.Value = slog.StringValue(renderLogLevel(lvl))
				}
			}
			return attr
		},
	})
	return slog.New(handler).With("component", component, "pid", os.Getpid()), level, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "trace":
		return levelTrace, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	case "crit", "critical", "fatal":
		return levelCrit, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", raw)
	}
}

func renderLogLevel(level slog.Level) string {
	switch {
	case level <= levelTrace:
		return "TRACE"
	case level <= slog.LevelDebug:
		return "DEBUG"
	case level <= slog.LevelInfo:
		return "INFO"
	case level <= slog.LevelWarn:
		return "WARN"
	case level <= slog.LevelError:
		return "ERROR"
	default:
		return "CRIT"
	}
}

func logCritical(logger *slog.Logger, msg string, args ...any) {
	logger.Log(context.Background(), levelCrit, msg, args...)
}
