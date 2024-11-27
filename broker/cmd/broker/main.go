package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/indexdata/go-utils/utils"
)

var HTTP_PORT = utils.GetEnvInt("HTTP_PORT", 8081)

func main() {
	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)

	log.Println("Server started on http://localhost:" + strconv.Itoa(HTTP_PORT))
	http.ListenAndServe(":"+strconv.Itoa(HTTP_PORT), mux)
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from " + r.URL.Path))
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}
