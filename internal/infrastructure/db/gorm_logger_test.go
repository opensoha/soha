package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/requestctx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	gormlogger "gorm.io/gorm/logger"
)

func TestGORMLoggerRecordsStructuredSlowQueryWithoutParameters(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := newGORMLogger(zap.New(core))
	ctx := requestctx.WithMetadata(context.Background(), requestctx.Metadata{
		RequestID: "request-1",
		TraceID:   "trace-1",
		SpanID:    "span-1",
	})

	logger.Trace(ctx, time.Now().Add(-250*time.Millisecond), func() (string, int64) {
		return "UPDATE docker_operations SET payload = $1 WHERE id = $2", 1
	}, nil)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Level != zapcore.WarnLevel || entry.LoggerName != "database" {
		t.Fatalf("entry level/name = %s/%q, want warn/database", entry.Level, entry.LoggerName)
	}
	fields := entry.ContextMap()
	if fields["event"] != "database.query.slow" || fields["rows"] != int64(1) {
		t.Fatalf("slow query fields = %#v", fields)
	}
	if fields["request_id"] != "request-1" || fields["trace_id"] != "trace-1" || fields["span_id"] != "span-1" {
		t.Fatalf("correlation fields = %#v", fields)
	}
	if fields["sql"] != "UPDATE docker_operations SET payload = $1 WHERE id = $2" {
		t.Fatalf("sql = %#v, want parameterized SQL", fields["sql"])
	}
}

func TestGORMLoggerPrioritizesErrorsAndIgnoresRecordNotFound(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := newGORMLogger(zap.New(core))

	logger.Trace(context.Background(), time.Now().Add(-250*time.Millisecond), func() (string, int64) {
		return "SELECT * FROM users WHERE token = $1", 0
	}, errors.New("query failed token=raw-secret"))
	logger.Trace(context.Background(), time.Now().Add(-250*time.Millisecond), func() (string, int64) {
		return "SELECT * FROM users WHERE id = $1", 0
	}, gormlogger.ErrRecordNotFound)

	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zapcore.ErrorLevel {
		t.Fatalf("entries = %#v, want one error", entries)
	}
	fields := entries[0].ContextMap()
	if fields["event"] != "database.query.failed" || fields["error"] != "query failed token=[REDACTED]" {
		t.Fatalf("error fields = %#v", fields)
	}
}

func TestGORMLoggerSkipsFastQueriesAtDefaultLevel(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := newGORMLogger(zap.New(core))
	logger.Trace(context.Background(), time.Now(), func() (string, int64) {
		return "SELECT 1", 1
	}, nil)
	if logs.Len() != 0 {
		t.Fatalf("log entries = %d, want 0", logs.Len())
	}
}

func TestGORMLoggerInfoModeRecordsFastQueries(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger, ok := newGORMLogger(zap.New(core)).LogMode(gormlogger.Info).(*gormZapLogger)
	if !ok {
		t.Fatal("LogMode() did not return *gormZapLogger")
	}
	logger.Trace(context.Background(), time.Now(), func() (string, int64) {
		return "SELECT 1", 1
	}, nil)
	if entries := logs.All(); len(entries) != 1 || entries[0].Level != zapcore.InfoLevel {
		t.Fatalf("entries = %#v, want one info log", entries)
	}
}

func TestGORMLoggerParamsFilterDropsBindValues(t *testing.T) {
	logger := newGORMLogger(nil)
	sql, params := logger.ParamsFilter(context.Background(), "SELECT * FROM users WHERE token = $1", "raw-secret")
	if sql != "SELECT * FROM users WHERE token = $1" || params != nil {
		t.Fatalf("ParamsFilter() = %q, %#v, want unchanged SQL and nil params", sql, params)
	}
}

func TestGORMLoggerLogModeReturnsCopy(t *testing.T) {
	logger := newGORMLogger(nil)
	muted, ok := logger.LogMode(gormlogger.Silent).(*gormZapLogger)
	if !ok {
		t.Fatal("LogMode() did not return *gormZapLogger")
	}
	if logger.level != gormlogger.Warn || muted.level != gormlogger.Silent || logger == muted {
		t.Fatalf("LogMode() original/muted = %v/%v", logger.level, muted.level)
	}
}
