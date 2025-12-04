package main

import (
	"context"
	"indexdata/directory/app"
	"log/slog"
	"os"
	"strings"

	slogctx "github.com/veqryn/slog-context"
	sloghttp "github.com/veqryn/slog-context/http"
)

func init() {
	// Configure log level from LOG_LEVEL env var (default: info)
	logLevel := slog.LevelInfo
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	// Configure format from LOG_FORMAT env var (default: text)
	// Set LOG_FORMAT=json for JSON output
	var baseHandler slog.Handler
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "json" {
		baseHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	} else {
		baseHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	}

	h := slogctx.NewHandler(
		baseHandler, // The next or final handler in the chain
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
