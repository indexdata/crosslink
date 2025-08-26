package main

import (
	"context"

	"fmt"
	"os"

	"github.com/indexdata/crosslink/broker/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// TODO parse command line arguments
	err := app.Archive(ctx, "LoanCompleted,CopyCompleted,Unfilled", "5d")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
