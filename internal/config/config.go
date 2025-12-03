// Copyright 2025 Base14. See LICENSE file for details.

package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config holds all configuration for the operator
type Config struct {
	// Environment is the deployment environment (development, production)
	Environment string `default:"production" split_words:"true"`

	// Logging configuration
	Logging LoggingConfig `split_words:"true"`
}

// LoggingConfig holds the configuration for logging
type LoggingConfig struct {
	// Level is the minimum log level (debug, info, warn, error)
	Level string `default:"info"`

	// Development puts the logger in development mode with human-readable output
	Development bool `default:"false"`
}

// IsDevelopment returns true if the environment is development
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// Load loads the configuration from environment variables with MEMGRAPH prefix
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("MEMGRAPH", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
