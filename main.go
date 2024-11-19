package main

import (
	"log"
	"net/http"

	"indexdata/directoryish/api"
)

func main() {
	server := api.NewServer()

	r := http.NewServeMux()

	h := api.HandlerFromMux(server, r)

	s := &http.Server{
		Handler: h,
		Addr:    "0.0.0.0:8080",
	}

	log.Fatal(s.ListenAndServe())
}
