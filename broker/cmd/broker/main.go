package main

import (
	"context"
	dbContext "github.com/indexdata/crosslink/broker/db"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"strconv"

	"github.com/indexdata/go-utils/utils"
)

var HTTP_PORT = utils.GetEnvInt("HTTP_PORT", 8081)

func init() {
	dbPool, err := pgxpool.New(context.Background(), "postgres://folio_admin:folio_admin@localhost:5432/okapi_modules")
	if err != nil {
		log.Fatalf("Unable to create pool to database: %v\n", err)
	}
	dbContext.SetDbPool(dbPool)
}

func main() {
	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)
	mux.HandleFunc("/externalapi/iso18626", handler.Iso18626PostHandler)

	log.Println("Server started on http://localhost:" + strconv.Itoa(HTTP_PORT))
	http.ListenAndServe(":"+strconv.Itoa(HTTP_PORT), mux)
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from " + r.URL.Path))
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}
