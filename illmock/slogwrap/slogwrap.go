package slogwrap

import (
	"log/slog"
	"os"

	"github.com/indexdata/go-utils/utils"
)

func slogEnable(enable bool) *slog.Logger {
	if enable {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.Default()
}

func SlogWrap() *slog.Logger {
	return slogEnable(utils.Must(utils.GetEnvBool("ENABLE_JSON_LOG", false)))
}
