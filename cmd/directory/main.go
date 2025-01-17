package main

import (
	"context"
	"indexdata/directoryish/app"
	"log/slog"
	"os"

	slogctx "github.com/veqryn/slog-context"
	sloghttp "github.com/veqryn/slog-context/http"
)

func init() {
	h := slogctx.NewHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}), // The next or final handler in the chain
		&slogctx.HandlerOptions{
			// Prependers will first add any sloghttp.With attributes,
			// then anything else Prepended to the ctx
			Prependers: []slogctx.AttrExtractor{
				sloghttp.ExtractAttrCollection, // our sloghttp middleware extractor
				slogctx.ExtractPrepended,       // for all other prepended attributes
			},
		},
	)
	slog.SetDefault(slog.New(h))
}

func main() {
	app.RunMigrateScripts()
	dbpool := app.InitDbPool()
	defer dbpool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.StartApp(ctx, dbpool)
}
