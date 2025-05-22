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
	err := app.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
