package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"

	"indexdata/directoryish/api"
	"indexdata/directoryish/db"
)

func main() {
	ctx := context.Background()

	swagger, err := api.GetSwagger()
	if err != nil {
		log.Fatal("Error loading API spec")
	}

	pgxConfig, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}

	pgxConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxUUID.Register(conn.TypeMap())
		return nil
	}

	dbpool, err := pgxpool.NewWithConfig(ctx, pgxConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create connection pool: %v\n", err)
		os.Exit(1)
	}
	defer dbpool.Close()

	queries := db.New(dbpool)
	impl := api.NewApiImpl(dbpool, queries)
	si := api.NewStrictHandler(impl, nil)
	m := http.NewServeMux()
	h := api.HandlerFromMux(si, m)
	mw := apiValidator.OapiRequestValidator(swagger)
	wrapped := mw(h)

	s := &http.Server{
		Handler: wrapped,
		Addr:    "0.0.0.0:8080",
	}

	log.Fatal(s.ListenAndServe())
}
