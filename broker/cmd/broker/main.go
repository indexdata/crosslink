package main

import (
	"context"
	"fmt"
	"github.com/indexdata/crosslink/broker/db"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"strconv"

	"github.com/indexdata/go-utils/utils"
)

var HTTP_PORT = utils.GetEnvInt("HTTP_PORT", 8081)
var DB_TYPE = utils.GetEnv("DB_TYPE", "postgres")
var DB_USER = utils.GetEnv("DB_USER", "folio_admin")
var DB_PASSWORD = utils.GetEnv("DB_PASSWORD", "folio_admin")
var DB_HOST = utils.GetEnv("DB_HOST", "localhost")
var DB_PORT = utils.GetEnv("DB_PORT", "5432")
var DB_DATABASE = utils.GetEnv("DB_DATABASE", "okapi_modules")
var connectionString = fmt.Sprintf("%s://%s:%s@%s:%s/%s", DB_TYPE, DB_USER, DB_PASSWORD, DB_HOST, DB_PORT, DB_DATABASE)

func main() {
	dbPool, err := pgxpool.New(context.Background(), connectionString)
	if err != nil {
		log.Fatalf("Unable to create pool to database: %v\n", err)
	}
	repo := new(repository.PostgresRepository)
	repo.DbPool = dbPool

	mux := http.NewServeMux()

	serviceHandler := http.HandlerFunc(HandleRequest)
	mux.Handle("/", serviceHandler)
	mux.HandleFunc("/healthz", HandleHealthz)
	mux.HandleFunc("/iso18626", handler.Iso18626PostHandler(repo))

	log.Println("Server started on http://localhost:" + strconv.Itoa(HTTP_PORT))
	http.ListenAndServe(":"+strconv.Itoa(HTTP_PORT), mux)
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from " + r.URL.Path))
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}
