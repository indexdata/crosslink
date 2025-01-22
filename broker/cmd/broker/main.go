package main

import (
	"context"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
)

func main() {
	app.RunMigrateScripts()
	pool := app.InitDbPool()
	eventRepo := app.CreateEventRepo(pool)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus := app.CreateEventBus(eventRepo)
	illRepo := app.CreateIllRepo(pool)
	iso18626Client := client.CreateIso18626Client(eventBus, illRepo)
	app.AddDefaultHandlers(eventBus, iso18626Client)
	app.StartEventBus(ctx, eventBus)
	app.StartApp(illRepo, eventBus)
}
