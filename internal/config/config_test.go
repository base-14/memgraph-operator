// Copyright 2025 Base14. See LICENSE file for details.

package config

import (
	"os"
	"testing"
)

func TestIsDevelopment(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		expected    bool
	}{
		{
			name:        "development environment",
			environment: "development",
			expected:    true,
		},
		{
			name:        "production environment",
			environment: "production",
			expected:    false,
		},
		{
			name:        "empty environment",
			environment: "",
			expected:    false,
		},
		{
			name:        "other environment",
			environment: "staging",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Environment: tt.environment,
			}

			if got := cfg.IsDevelopment(); got != tt.expected {
				t.Errorf("IsDevelopment() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	// Save original env vars
	originalEnv := os.Getenv("MEMGRAPH_ENVIRONMENT")
	originalLogLevel := os.Getenv("MEMGRAPH_LOGGING_LEVEL")
	originalLogDev := os.Getenv("MEMGRAPH_LOGGING_DEVELOPMENT")

	// Cleanup after test
	defer func() {
		_ = os.Setenv("MEMGRAPH_ENVIRONMENT", originalEnv)
		_ = os.Setenv("MEMGRAPH_LOGGING_LEVEL", originalLogLevel)
		_ = os.Setenv("MEMGRAPH_LOGGING_DEVELOPMENT", originalLogDev)
	}()

	tests := []struct {
		name           string
		envVars        map[string]string
		expectedEnv    string
		expectedLevel  string
		expectedDevLog bool
	}{
		{
			name:           "default values",
			envVars:        map[string]string{},
			expectedEnv:    "production",
			expectedLevel:  "info",
			expectedDevLog: false,
		},
		{
			name: "custom environment",
			envVars: map[string]string{
				"MEMGRAPH_ENVIRONMENT": "development",
			},
			expectedEnv:    "development",
			expectedLevel:  "info",
			expectedDevLog: false,
		},
		{
			name: "custom logging",
			envVars: map[string]string{
				"MEMGRAPH_LOGGING_LEVEL":       "debug",
				"MEMGRAPH_LOGGING_DEVELOPMENT": "true",
			},
			expectedEnv:    "production",
			expectedLevel:  "debug",
			expectedDevLog: true,
		},
		{
			name: "all custom values",
			envVars: map[string]string{
				"MEMGRAPH_ENVIRONMENT":         "development",
				"MEMGRAPH_LOGGING_LEVEL":       "warn",
				"MEMGRAPH_LOGGING_DEVELOPMENT": "true",
			},
			expectedEnv:    "development",
			expectedLevel:  "warn",
			expectedDevLog: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			_ = os.Unsetenv("MEMGRAPH_ENVIRONMENT")
			_ = os.Unsetenv("MEMGRAPH_LOGGING_LEVEL")
			_ = os.Unsetenv("MEMGRAPH_LOGGING_DEVELOPMENT")

			// Set test env vars
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Environment != tt.expectedEnv {
				t.Errorf("Environment = %v, want %v", cfg.Environment, tt.expectedEnv)
			}

			if cfg.Logging.Level != tt.expectedLevel {
				t.Errorf("Logging.Level = %v, want %v", cfg.Logging.Level, tt.expectedLevel)
			}

			if cfg.Logging.Development != tt.expectedDevLog {
				t.Errorf("Logging.Development = %v, want %v", cfg.Logging.Development, tt.expectedDevLog)
			}
		})
	}
}

func TestLoadWithInvalidEnv(t *testing.T) {
	// Save original env var
	originalLogDev := os.Getenv("MEMGRAPH_LOGGING_DEVELOPMENT")

	// Cleanup after test
	defer func() {
		_ = os.Setenv("MEMGRAPH_LOGGING_DEVELOPMENT", originalLogDev)
	}()

	// Set an invalid boolean value
	_ = os.Setenv("MEMGRAPH_LOGGING_DEVELOPMENT", "not-a-boolean")

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error for invalid boolean, got nil")
	}
}
