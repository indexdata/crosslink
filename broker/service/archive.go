package service

import (
	"strconv"
	"strings"
	"time"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

func ParseDurationWithDays(archiveDelay string) (time.Duration, error) {
	if !strings.HasSuffix(archiveDelay, "d") {
		return time.ParseDuration(archiveDelay)
	}
	days, err := strconv.Atoi(strings.TrimSuffix(archiveDelay, "d"))
	if err != nil {
		return 0, err
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

func Archive(ctx extctx.ExtendedContext, illRepo ill_db.IllRepo, statusList string, archiveDelay string, background bool) error {
	delayInterval, err := ParseDurationWithDays(archiveDelay)
	if err != nil {
		return err
	}
	var fromTime = time.Now().Add(-delayInterval)
	if !background {
		return illRepo.CallArchiveIllTransactionByDateAndStatus(ctx, fromTime, strings.Split(statusList, ","))
	}
	go func() {
		err := illRepo.CallArchiveIllTransactionByDateAndStatus(ctx, fromTime, strings.Split(statusList, ","))
		if err != nil {
			ctx.Logger().Error("failed to archive ill transactions", "error", err)
		}
	}()
	return nil
}
