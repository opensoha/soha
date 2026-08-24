package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/opensoha/soha/internal/platform/redaction"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	defaultSlowQueryThreshold = 200 * time.Millisecond
	maxDatabaseMessageBytes   = 2048
	maxDatabaseSQLBytes       = 8192
)

type gormZapLogger struct {
	logger        *zap.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

var (
	_ gormlogger.Interface = (*gormZapLogger)(nil)
	_ gorm.ParamsFilter    = (*gormZapLogger)(nil)
)

func newGORMLogger(logger *zap.Logger) *gormZapLogger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &gormZapLogger{
		logger:        logger.Named("database"),
		level:         gormlogger.Warn,
		slowThreshold: defaultSlowQueryThreshold,
	}
}

func (l *gormZapLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *gormZapLogger) Info(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Info {
		l.logger.Info("database message", databaseFields(ctx, "database.message.info", zap.String("detail", databaseMessage(message, args...)))...)
	}
}

func (l *gormZapLogger) Warn(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Warn {
		l.logger.Warn("database warning", databaseFields(ctx, "database.message.warning", zap.String("detail", databaseMessage(message, args...)))...)
	}
}

func (l *gormZapLogger) Error(ctx context.Context, message string, args ...interface{}) {
	if l.level >= gormlogger.Error {
		l.logger.Error("database error", databaseFields(ctx, "database.message.error", zap.String("detail", databaseMessage(message, args...)))...)
	}
}

func (l *gormZapLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	if errors.Is(err, gormlogger.ErrRecordNotFound) {
		return
	}

	elapsed := time.Since(begin)
	switch {
	case err != nil && l.level >= gormlogger.Error:
		sql, rows := query()
		fields := databaseQueryFields(ctx, "database.query.failed", elapsed, rows, sql)
		fields = append(fields, zap.String("error", redaction.LogText(err.Error(), maxDatabaseMessageBytes)))
		l.logger.Error("database query failed", fields...)
	case elapsed > l.slowThreshold && l.slowThreshold > 0 && l.level >= gormlogger.Warn:
		sql, rows := query()
		fields := databaseQueryFields(ctx, "database.query.slow", elapsed, rows, sql)
		fields = append(fields, zap.Float64("slow_threshold_ms", float64(l.slowThreshold)/float64(time.Millisecond)))
		l.logger.Warn("slow database query", fields...)
	case l.level >= gormlogger.Info:
		sql, rows := query()
		l.logger.Info("database query completed", databaseQueryFields(ctx, "database.query.completed", elapsed, rows, sql)...)
	}
}

// ParamsFilter keeps GORM bind values out of the SQL rendered for Trace.
func (l *gormZapLogger) ParamsFilter(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}

func databaseFields(ctx context.Context, event string, extra ...zap.Field) []zap.Field {
	fields := []zap.Field{zap.String("event", event)}
	fields = append(fields, requestctx.CorrelationFields(requestctx.FromContext(ctx))...)
	return append(fields, extra...)
}

func databaseQueryFields(ctx context.Context, event string, elapsed time.Duration, rows int64, sql string) []zap.Field {
	return databaseFields(ctx, event,
		zap.Float64("latency_ms", float64(elapsed)/float64(time.Millisecond)),
		zap.Int64("rows", rows),
		zap.String("sql", redaction.LogText(sql, maxDatabaseSQLBytes)),
	)
}

func databaseMessage(message string, args ...interface{}) string {
	return redaction.LogText(fmt.Sprintf(message, args...), maxDatabaseMessageBytes)
}
