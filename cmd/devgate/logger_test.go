package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
)

func TestNewLoggerJSONFormat(t *testing.T) {
	var output bytes.Buffer

	logger, err := newLogger(&output, config.LogFormatJSON, config.LogLevelInfo)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	logger.Info("request completed", "status", 200)

	var got struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Status  int    `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON log %q: %v", output.String(), err)
	}

	if got.Level != "INFO" {
		t.Errorf("log level = %q, want %q", got.Level, "INFO")
	}
	if got.Message != "request completed" {
		t.Errorf("log message = %q, want %q", got.Message, "request completed")
	}
	if got.Status != 200 {
		t.Errorf("log status = %d, want %d", got.Status, 200)
	}
}

func TestNewLoggerTextFormat(t *testing.T) {
	var output bytes.Buffer

	logger, err := newLogger(&output, config.LogFormatText, config.LogLevelInfo)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	logger.Info("request completed", "status", 200)

	got := output.String()
	for _, want := range []string{
		"level=INFO",
		`msg="request completed"`,
		"status=200",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text log %q does not contain %q", got, want)
		}
	}
}

func TestNewLoggerLevel(t *testing.T) {
	tests := []struct {
		name          string
		configured    config.LogLevel
		disabledLevel slog.Level
		enabledLevel  slog.Level
	}{
		{
			name:          "debug",
			configured:    config.LogLevelDebug,
			disabledLevel: slog.LevelDebug - 1,
			enabledLevel:  slog.LevelDebug,
		},
		{
			name:          "info",
			configured:    config.LogLevelInfo,
			disabledLevel: slog.LevelDebug,
			enabledLevel:  slog.LevelInfo,
		},
		{
			name:          "warn",
			configured:    config.LogLevelWarn,
			disabledLevel: slog.LevelInfo,
			enabledLevel:  slog.LevelWarn,
		},
		{
			name:          "error",
			configured:    config.LogLevelError,
			disabledLevel: slog.LevelWarn,
			enabledLevel:  slog.LevelError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, err := newLogger(&bytes.Buffer{}, config.LogFormatText, test.configured)
			if err != nil {
				t.Fatalf("newLogger() error = %v", err)
			}

			ctx := context.Background()
			if logger.Enabled(ctx, test.disabledLevel) {
				t.Errorf("logger.Enabled(%s) = true, want false", test.disabledLevel)
			}
			if !logger.Enabled(ctx, test.enabledLevel) {
				t.Errorf("logger.Enabled(%s) = false, want true", test.enabledLevel)
			}
		})
	}
}

func TestNewLoggerRejectsUnsupportedConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		format      config.LogFormat
		level       config.LogLevel
		wantMessage string
	}{
		{
			name:        "format",
			format:      config.LogFormat("xml"),
			level:       config.LogLevelInfo,
			wantMessage: `unsupported log format "xml"`,
		},
		{
			name:        "level",
			format:      config.LogFormatText,
			level:       config.LogLevel("verbose"),
			wantMessage: `unsupported log level "verbose"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, err := newLogger(&bytes.Buffer{}, test.format, test.level)
			if err == nil {
				t.Fatal("newLogger() error = nil, want configuration error")
			}
			if logger != nil {
				t.Errorf("newLogger() logger = %v, want nil", logger)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("newLogger() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}
