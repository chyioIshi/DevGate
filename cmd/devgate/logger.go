package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/chyioishi/devgate/internal/config"
)

func newLogger(output io.Writer, format config.LogFormat, level config.LogLevel) (*slog.Logger, error) {
	var logHandler slog.Handler
	var slogLevel slog.Level

	switch level {
	case config.LogLevelDebug:
		slogLevel = slog.LevelDebug
	case config.LogLevelInfo:
		slogLevel = slog.LevelInfo
	case config.LogLevelWarn:
		slogLevel = slog.LevelWarn
	case config.LogLevelError:
		slogLevel = slog.LevelError
	default:
		return nil, fmt.Errorf("unsupported log level %q", level)
	}
	logOptions := &slog.HandlerOptions{Level: slogLevel}
	switch format {
	case config.LogFormatText:
		logHandler = slog.NewTextHandler(output, logOptions)
	case config.LogFormatJSON:
		logHandler = slog.NewJSONHandler(output, logOptions)
	default:
		return nil, fmt.Errorf("unsupported log format %q", format)
	}
	return slog.New(logHandler), nil
}
