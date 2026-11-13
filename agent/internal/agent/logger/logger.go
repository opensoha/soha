package logger

import (
	"fmt"
	"slices"
	"strings"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg cfgpkg.LoggerConfig) (*zap.Logger, error) {
	zapConfig, err := buildConfig(cfg)
	if err != nil {
		return nil, err
	}
	return buildLogger(zapConfig)
}

func buildLogger(config zap.Config) (*zap.Logger, error) {
	if config.Encoding != "json" {
		return config.Build()
	}

	config.EncoderConfig.TimeKey = zapcore.OmitKey
	config.EncoderConfig.LevelKey = zapcore.OmitKey
	config.EncoderConfig.NameKey = zapcore.OmitKey
	config.EncoderConfig.CallerKey = zapcore.OmitKey
	config.EncoderConfig.MessageKey = zapcore.OmitKey
	config.EncoderConfig.StacktraceKey = zapcore.OmitKey
	config.InitialFields = nil

	return config.Build(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return &orderedJSONCore{Core: core}
	}))
}

type orderedJSONCore struct {
	zapcore.Core
	fields []zapcore.Field
}

func (c *orderedJSONCore) With(fields []zapcore.Field) zapcore.Core {
	contextFields := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	contextFields = append(contextFields, c.fields...)
	contextFields = append(contextFields, fields...)
	return &orderedJSONCore{Core: c.Core, fields: contextFields}
}

func (c *orderedJSONCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *orderedJSONCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	allFields := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	allFields = append(allFields, c.fields...)
	allFields = append(allFields, fields...)
	return c.Core.Write(entry, orderedJSONFields(entry, allFields))
}

func orderedJSONFields(entry zapcore.Entry, fields []zapcore.Field) []zapcore.Field {
	ordered := make([]zapcore.Field, 0, len(fields)+8)
	if !entry.Time.IsZero() {
		ordered = append(ordered, zap.Time("timestamp", entry.Time))
	}
	ordered = append(ordered, zap.String("level", entry.Level.String()))
	if entry.LoggerName != "" {
		ordered = append(ordered, zap.String("component", entry.LoggerName))
	}
	ordered = append(ordered, zap.String("service", "soha-agent"))
	if entry.Caller.Defined {
		ordered = append(ordered, zap.String("caller", entry.Caller.TrimmedPath()))
	}
	ordered = append(ordered, zap.String("message", entry.Message))
	for _, field := range fields {
		if !canonicalEntryField(field.Key) {
			ordered = append(ordered, field)
		}
	}
	if entry.Stack != "" {
		ordered = append(ordered, zap.String("stacktrace", entry.Stack))
	}

	slices.SortStableFunc(ordered, func(left, right zapcore.Field) int {
		return jsonFieldOrder(left.Key) - jsonFieldOrder(right.Key)
	})
	return ordered
}

func canonicalEntryField(key string) bool {
	switch key {
	case "timestamp", "level", "component", "service", "caller", "message", "stacktrace":
		return true
	default:
		return false
	}
}

func jsonFieldOrder(key string) int {
	switch key {
	case "timestamp":
		return 0
	case "level":
		return 1
	case "request_id":
		return 2
	case "component":
		return 3
	case "latency_ms":
		return 4
	case "service":
		return 5
	case "caller":
		return 6
	case "event":
		return 7
	case "message":
		return 8
	case "stacktrace":
		return 10
	default:
		return 9
	}
}

func buildConfig(cfg cfgpkg.LoggerConfig) (zap.Config, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(cfg.Level)))); err != nil {
		return zap.Config{}, fmt.Errorf("parse logger level %q: %w", cfg.Level, err)
	}

	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	if format != "json" && format != "console" {
		return zap.Config{}, fmt.Errorf("unsupported logger format %q", cfg.Format)
	}

	encoder := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "component",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     encodeUTC,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}
	if format == "console" {
		encoder.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	return zap.Config{
		Level:             zap.NewAtomicLevelAt(level),
		Development:       false,
		DisableCaller:     false,
		DisableStacktrace: false,
		Sampling:          nil,
		Encoding:          format,
		EncoderConfig:     encoder,
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stderr"},
		InitialFields:     map[string]any{"service": "soha-agent"},
	}, nil
}

func encodeUTC(value time.Time, encoder zapcore.PrimitiveArrayEncoder) {
	zapcore.RFC3339NanoTimeEncoder(value.UTC(), encoder)
}
