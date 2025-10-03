package common

import (
	"context"
	"log/slog"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
)

func TestCreateAndGetLogger(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{RequestId: "r1", TransactionId: "t1", EventId: "e1"})
	logger := ctx.Logger()
	if logger == slog.Default() {
		t.Error("Should not be the same as default logger")
	}
}
