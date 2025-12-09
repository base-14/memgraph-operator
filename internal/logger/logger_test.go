// Copyright 2025 Base14. See LICENSE file for details.

package logger

import (
	"testing"

	"github.com/base14/memgraph-operator/internal/config"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		shouldFail bool
	}{
		{
			name: "production mode with info level",
			cfg: &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       "info",
					Development: false,
				},
			},
			shouldFail: false,
		},
		{
			name: "development mode with debug level",
			cfg: &config.Config{
				Environment: "development",
				Logging: config.LoggingConfig{
					Level:       "debug",
					Development: true,
				},
			},
			shouldFail: false,
		},
		{
			name: "production mode with warn level",
			cfg: &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       "warn",
					Development: false,
				},
			},
			shouldFail: false,
		},
		{
			name: "production mode with error level",
			cfg: &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       "error",
					Development: false,
				},
			},
			shouldFail: false,
		},
		{
			name: "production mode with unknown level defaults to info",
			cfg: &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       "unknown",
					Development: false,
				},
			},
			shouldFail: false,
		},
		{
			name: "development enabled but not in development environment",
			cfg: &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       "debug",
					Development: true,
				},
			},
			shouldFail: false,
		},
		{
			name: "development environment without development logging",
			cfg: &config.Config{
				Environment: "development",
				Logging: config.LoggingConfig{
					Level:       "info",
					Development: false,
				},
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.cfg)

			if tt.shouldFail {
				if err == nil {
					t.Error("New() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if logger == nil {
				t.Error("New() returned nil logger")
			}

			// Verify the logger can be used
			logger.Info("test message")
			logger.Debug("debug message")
			logger.Warn("warn message")
			logger.Error("error message")
		})
	}
}

func TestNewWithAllLogLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "unknown", ""}

	for _, level := range levels {
		t.Run("level_"+level, func(t *testing.T) {
			cfg := &config.Config{
				Environment: "production",
				Logging: config.LoggingConfig{
					Level:       level,
					Development: false,
				},
			}

			logger, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v for level %s", err, level)
			}

			if logger == nil {
				t.Errorf("New() returned nil logger for level %s", level)
			}
		})
	}
}
