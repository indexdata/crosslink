package main

import (
	"context"
	"flag"

	"fmt"
	"os"

	"github.com/indexdata/crosslink/broker/app"
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
	flag.StringVar(&duration, "duration", "120h", "archive transactions older than this duration, for example 48h")
	flag.Parse()

	return app.Archive(ctx, statusList, duration)
}
