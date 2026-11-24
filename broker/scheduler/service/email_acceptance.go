package sched_service

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *EmailSenderService) recordEmailAcceptance(ctx common.ExtendedContext, runID string) error {
	const maxAttempts = 3
	retryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	loggerArgs := ctx.LoggerArgs()
	writeCtx := common.CreateExtCtxWithArgs(retryCtx, &loggerArgs)

	for attempt := 1; ; attempt++ {
		if err := writeCtx.Err(); err != nil {
			return err
		}
		err := s.deliveryRepo.RecordBatchEmailAcceptance(writeCtx, runID)
		if err == nil || attempt == maxAttempts || !isTransientAcceptanceError(err) {
			return err
		}
		select {
		case <-writeCtx.Done():
			return writeCtx.Err()
		case <-time.After(time.Duration(attempt) * 50 * time.Millisecond):
		}
	}
}

func isTransientAcceptanceError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.SerializationFailure, pgerrcode.DeadlockDetected, pgerrcode.StatementCompletionUnknown,
			pgerrcode.AdminShutdown, pgerrcode.CrashShutdown, pgerrcode.CannotConnectNow:
			return true
		}
		return pgerrcode.IsConnectionException(pgErr.Code)
	}
	var networkErr *net.OpError
	return pgconn.SafeToRetry(err) || pgconn.Timeout(err) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.As(err, &networkErr)
}
