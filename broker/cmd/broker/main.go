package main

import (
	"context"
	"github.com/indexdata/crosslink/broker/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.StartApp(ctx)
}
