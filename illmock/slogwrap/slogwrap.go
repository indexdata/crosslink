package slogwrap

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

func SlogWrap() *slog.Logger {
	enableJsonLog := os.Getenv("ENABLE_JSON_LOG")
	if enableJsonLog != "" {
		v, err := strconv.ParseBool(enableJsonLog)
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
