package netutil

import (
	"log/slog"
	"net/http"

	"github.com/indexdata/crosslink/illmock/slogwrap"
)

var log *slog.Logger = slogwrap.SlogWrap()

func WriteHttpResponse(w http.ResponseWriter, buf []byte) {
	_, err := w.Write(buf)
	if err != nil {
		log.Warn("writeResponse", "error", err.Error())
	}
}
