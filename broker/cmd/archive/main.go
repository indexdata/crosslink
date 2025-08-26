package main

import (
	"context"
	"flag"

	"fmt"
	"os"

	"github.com/indexdata/crosslink/broker/app"
)

func main() {
	var statusList string
	var duration string
	flag.StringVar(&statusList, "statuses", "LoanCompleted,CopyCompleted,Unfilled", "comma separated list of statuses to archive")
	flag.StringVar(&duration, "duration", "5d", "archive transactions older than this duration, for example 10d")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Archive(ctx, statusList, duration)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
