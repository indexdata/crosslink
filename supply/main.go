package main

import (
	"context"

	"github.com/indexdata/mod-dms/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Start(ctx)
}
