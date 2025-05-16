package main

import (
	"context"

	"fmt"
	"os"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/vcs"
)

func main() {
	fmt.Println("Starting broker:", vcs.GetCommit())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
