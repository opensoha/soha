package logger

import (
	"strings"

	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg cfgpkg.LoggerConfig) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(strings.ToLower(cfg.Level))); err != nil {
		level = zapcore.InfoLevel
	}

	var zapConfig zap.Config
	if strings.ToLower(cfg.Format) == "json" {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)
	return zapConfig.Build()
}
