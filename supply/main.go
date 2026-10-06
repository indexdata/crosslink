package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/indexdata/crosslink/supply/app"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		slog.Error("Supply failed", "error", err)
		cancel()
		os.Exit(1)
	}
}
