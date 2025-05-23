package common

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
)

type ExtendedContext interface {
	context.Context
	// return logger associated with this context
	Logger() *slog.Logger
	// create new instance backed by the same context and log handler but with new log args
	WithArgs(args *LoggerArgs) ExtendedContext
}

func Must[T any](ctx ExtendedContext, handler func() (ret T, err error), errMsg string) T {
	return MustHttp(ctx, nil, handler, errMsg)
}

func MustHttp[T any](ctx ExtendedContext, w http.ResponseWriter, handler func() (ret T, err error), errMsg string) T {
	ret, err := handler()
	if err != nil {
		if errMsg != "" {
			ctx.Logger().Error(errMsg, "error", err)
			if w != nil {
				http.Error(w, errMsg, http.StatusInternalServerError)
			}
			panic(errMsg)
		} else {
			ctx.Logger().Error(err.Error(), "error", err)
			if w != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			panic(err)
		}
	}
	return ret
}

var DefaultLogHandler slog.Handler = slog.NewTextHandler(os.Stdout, nil)

type LoggerArgs struct {
	RequestId     string
	TransactionId string
	EventId       string
	Other         map[string]string
}

type _ExtCtxImpl struct {
	context.Context
	logger     *slog.Logger
	logHandler slog.Handler
}

func (ctx *_ExtCtxImpl) Logger() *slog.Logger {
	return ctx.logger
}

func (ctx *_ExtCtxImpl) WithArgs(args *LoggerArgs) ExtendedContext {
	return CreateExtCtxWithLogArgsAndHandler(ctx.Context, args, ctx.logHandler)
}

func CreateExtCtxWithArgs(ctx context.Context, args *LoggerArgs) ExtendedContext {
	return CreateExtCtxWithLogArgsAndHandler(ctx, args, DefaultLogHandler)
}

func CreateExtCtxWithLogArgsAndHandler(ctx context.Context, args *LoggerArgs, logHandler slog.Handler) ExtendedContext {
	var extctx _ExtCtxImpl
	extctx.Context = ctx
	extctx.logHandler = logHandler
	extctx.logger = createChildLoggerWithArgs(slog.New(logHandler), args)
	return &extctx
}

func createChildLoggerWithArgs(logger *slog.Logger, args *LoggerArgs) *slog.Logger {
	loggerWithArgs := logger.With("process", uuid.New().String())
	if args != nil {
		if args.RequestId != "" {
			loggerWithArgs = loggerWithArgs.With("requestId", args.RequestId)
		}
		if args.TransactionId != "" {
			loggerWithArgs = loggerWithArgs.With("transactionId", args.TransactionId)
		}
		if args.EventId != "" {
			loggerWithArgs = loggerWithArgs.With("eventId", args.EventId)
		}
		for k, v := range args.Other {
			loggerWithArgs = loggerWithArgs.With(k, v)
		}
	}
	return loggerWithArgs
}
