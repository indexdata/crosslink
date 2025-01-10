package main

import (
	"context"
	"github.com/indexdata/crosslink/broker/app"
)

func main() {
	app.RunMigrateScripts()
	pool := app.InitDbPool()
	eventRepo := app.CreateEventRepo(pool)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus := app.InitEventBus(ctx, eventRepo)
	illRepo := app.CreateIllRepo(pool)
	app.StartApp(illRepo, eventBus)
}
