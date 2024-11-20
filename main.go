package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"indexdata/directoryish/api"
	"indexdata/directoryish/db"
)

func main() {
	ctx := context.Background()

	dbpool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create connection pool: %v\n", err)
		os.Exit(1)
	}
	defer dbpool.Close()

	queries := db.New(dbpool)

	server := api.NewServer(queries, ctx)

	r := http.NewServeMux()

	h := api.HandlerFromMux(server, r)

	s := &http.Server{
		Handler: h,
		Addr:    "0.0.0.0:8080",
	}

	log.Fatal(s.ListenAndServe())
}
