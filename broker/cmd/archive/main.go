package main

import (
	"context"
	"flag"

	"fmt"
	"os"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var statusList string
	var duration string
	flag.StringVar(&statusList, "statuses", "LoanCompleted,CopyCompleted,Unfilled", "comma separated list of statuses to archive")
	flag.StringVar(&duration, "duration", "5d", "archive transactions older than this duration, for example 2d")
	flag.Parse()
	err := app.RunDbUp()
	if err != nil {
		return err
	}
	pool, err := app.InitDbPool()
	if err != nil {
		return err
	}
	defer pool.Close()
	illRepo := ill_db.CreateIllRepo(pool)
	logParams := map[string]string{"method": "PostArchiveIllTransactions", "ArchiveDelay": duration, "ArchiveStatus": statusList}
	ectx := common.CreateExtCtxWithArgs(ctx, &common.LoggerArgs{
		Other: logParams,
	})
	return service.Archive(ectx, illRepo, statusList, duration, false)
}
