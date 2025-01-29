package slogwrap

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

func slogEnable(enable string) *slog.Logger {
	if enable != "" {
		v, err := strconv.ParseBool(enable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		if v {
			return slog.New(slog.NewJSONHandler(os.Stdout, nil))
		}
	}
	return slog.Default()
}

func SlogWrap() *slog.Logger {
	return slogEnable(os.Getenv("ENABLE_JSON_LOG"))
}
