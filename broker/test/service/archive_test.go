package service

import (
	"context"
	"testing"
	"time"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/stretchr/testify/assert"
)

func TestArchiveInvalidDelay(t *testing.T) {
	err := service.Archive(nil, nil, "LoanCompleted,CopyCompleted,Unfilled", "2x", false)
	assert.Error(t, err)
	assert.Equal(t, "time: unknown unit \"x\" in duration \"2x\"", err.Error())
}

func TestInvalidUnit(t *testing.T) {
	_, err := service.ParseDurationWithDays("2x")
	assert.Error(t, err)
	assert.Equal(t, "time: unknown unit \"x\" in duration \"2x\"", err.Error())
}

func TestValidDelayDays(t *testing.T) {
	duration, err := service.ParseDurationWithDays("5d")
	assert.NoError(t, err)
	assert.Equal(t, 5*24*time.Hour, duration)
}

func TestInvalidDelayDays(t *testing.T) {
	_, err := service.ParseDurationWithDays("ad")
	assert.Error(t, err)
	assert.Equal(t, "strconv.Atoi: parsing \"a\": invalid syntax", err.Error())
}

func TestValidDelayHours(t *testing.T) {
	duration, err := service.ParseDurationWithDays("5h")
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Hour, duration)
}

func TestArchiveDbError(t *testing.T) {
	illrepo := &mocks.MockIllRepositoryError{}

	logParams := map[string]string{"method": "PostArchiveIllTransactions", "ArchiveDelay": "5h", "ArchiveStatus": "LoanCompleted"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ectx := extctx.CreateExtCtxWithArgs(ctx, &extctx.LoggerArgs{
		Other: logParams,
	})
	err := service.Archive(ectx, illrepo, "LoanCompleted", "5h", false)
	assert.Error(t, err)
	assert.Equal(t, "DB error", err.Error())
}

func TestArchiveBackground(t *testing.T) {
	illrepo := &mocks.MockIllRepositoryError{}

	logParams := map[string]string{"method": "PostArchiveIllTransactions", "ArchiveDelay": "5h", "ArchiveStatus": "LoanCompleted"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ectx := extctx.CreateExtCtxWithArgs(ctx, &extctx.LoggerArgs{
		Other: logParams,
	})
	err := service.Archive(ectx, illrepo, "LoanCompleted", "5h", true)
	assert.NoError(t, err)
}
