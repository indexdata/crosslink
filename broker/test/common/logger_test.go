package common

import (
	"context"
	"github.com/indexdata/crosslink/broker/common"
	"testing"
)

func TestCreateAndGetLogger(t *testing.T) {
	ctx := common.GetContextWithLogger(context.Background(), "r1", "t1", "e1")
	logger := common.GetLoggerFromContext(ctx)
	if logger == common.Logger {
		t.Error("Should not be the same as default logger")
	}
}
