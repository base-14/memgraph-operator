// Copyright 2025 Base14. See LICENSE file for details.

package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/base14/memgraph-operator/internal/config"
)

// New creates a new zap logger based on configuration
func New(cfg *config.Config) (*zap.Logger, error) {
	var zapCfg zap.Config

	if cfg.Logging.Development || cfg.IsDevelopment() {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
	}

	// Set log level
	switch cfg.Logging.Level {
	case "debug":
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	default:
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	return zapCfg.Build()
}
