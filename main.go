package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"
	slogctx "github.com/veqryn/slog-context"
	sloghttp "github.com/veqryn/slog-context/http"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"

	"indexdata/directoryish/api"
	"indexdata/directoryish/db"
)

func init() {
	h := slogctx.NewHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}), // The next or final handler in the chain
		&slogctx.HandlerOptions{
			// Prependers will first add any sloghttp.With attributes,
			// then anything else Prepended to the ctx
			Prependers: []slogctx.AttrExtractor{
				sloghttp.ExtractAttrCollection, // our sloghttp middleware extractor
				slogctx.ExtractPrepended,       // for all other prepended attributes
			},
		},
	)
	slog.SetDefault(slog.New(h))
}

func httpLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(sloghttp.With(r.Context(), "path", r.URL.Path))
		slogctx.Info(r.Context(), "Request", "method", r.Method)
		next.ServeHTTP(w, r)
	})
}

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
	validationMiddleware := apiValidator.OapiRequestValidator(swagger)
	wrapped := httpLoggingMiddleware(validationMiddleware(h))

	log.Println("Starting directoryish...")
	s := &http.Server{
		Handler: wrapped,
		Addr:    "0.0.0.0:8080",
	}

	log.Fatal(s.ListenAndServe())
}
