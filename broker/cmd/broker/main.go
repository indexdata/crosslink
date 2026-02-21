package main

import (
	"context"

	"log"
	"os"

	"github.com/indexdata/crosslink/broker/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := run(ctx, os.Args[1:])
	if err != nil {
		log.Printf("broker startup failed: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "db-up" {
		return app.RunDbUp()
	}
	return app.Run(ctx)
}
