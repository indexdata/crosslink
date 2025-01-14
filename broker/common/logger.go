package common

import (
	"context"
	"github.com/google/uuid"
	"log/slog"
	"os"
)

type ContextKey string

const (
	ContextKeyLogger ContextKey = "logger"
)

var Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func GetContextWithLogger(ctx context.Context, requestId string, transactionId string, eventId string) context.Context {
	LoggerWithArgs := Logger.With("process", uuid.New().String())
	if requestId != "" {
		LoggerWithArgs = LoggerWithArgs.With("requestId", requestId)
	}
	if transactionId != "" {
		LoggerWithArgs = LoggerWithArgs.With("transactionId", transactionId)
	}
	if eventId != "" {
		LoggerWithArgs = LoggerWithArgs.With("eventId", eventId)
	}
	ctx = context.WithValue(ctx, ContextKeyLogger, LoggerWithArgs)
	return ctx
}

func GetLoggerFromContext(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(ContextKeyLogger).(*slog.Logger)
	if !ok { // If no logger in context then return default value
		return Logger
	} else {
		return logger
	}
}
