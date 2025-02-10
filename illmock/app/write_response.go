package app

import (
	"net/http"
)

func writeHttpResponse(w http.ResponseWriter, buf []byte) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf)
	if err != nil {
		log.Warn("writeResponse", "error", err.Error())
	}
}
