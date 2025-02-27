package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/indexdata/crosslink/broker/app"
)

//go:embed commit.txt
var CommitId string

func GetCommit() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return CommitId
}

func main() {
	fmt.Println("Starting broker:", GetCommit())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
