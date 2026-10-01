package main

import (
	"context"

	"github.com/indexdata/crosslink/supply/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Start(ctx)
}
