package common

import (
	"context"
	"log/slog"
	"testing"

	extctx "github.com/indexdata/crosslink/broker/common"
)

func TestCreateAndGetLogger(t *testing.T) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{"r1", "t1", "e1"})
	logger := ctx.Logger()
	if logger == slog.Default() {
		t.Error("Should not be the same as default logger")
	}
}
