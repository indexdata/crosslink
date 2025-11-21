package app

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apiValidator "github.com/oapi-codegen/nethttp-middleware"
	slogctx "github.com/veqryn/slog-context"
	sloghttp "github.com/veqryn/slog-context/http"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"

	"indexdata/directoryish/api"
	"indexdata/directoryish/db"
)

var Host = cmp.Or(os.Getenv("HOST"), "localhost")
var Port = cmp.Or(os.Getenv("PORT"), "8086")
var ConnectionString = cmp.Or(os.Getenv("DATABASE_URL"), "postgresql://postgres:directoryish@localhost:54322/directoryish")
var MigrationsFolder = "file://migrations"
var BasePath = "/rsdir"

func httpLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(sloghttp.With(r.Context(), "path", r.URL.Path))
		slogctx.Info(r.Context(), "Request", "method", r.Method)
		next.ServeHTTP(w, r)
	})
}

func StartApp(ctx context.Context, dbpool *pgxpool.Pool) {
	handler := InitHandler(ctx, dbpool)
	addr := fmt.Sprintf("%s:%s", Host, Port)
	slog.InfoContext(ctx, "Starting directoryish", "addr", addr)
	s := &http.Server{
		Handler: handler,
		Addr:    addr,
	}

	if err := s.ListenAndServe(); err != nil {
		slog.ErrorContext(ctx, "Server failed", "error", err)
		os.Exit(1)
	}
}

func InitHandler(ctx context.Context, dbpool *pgxpool.Pool) http.Handler {
	swagger, err := api.GetSwagger()
	if err != nil {
		slog.ErrorContext(ctx, "Error loading API spec", "error", err)
		os.Exit(1)
	}

	queries := db.New(dbpool)
	impl := api.NewApiImpl(dbpool, queries)
	si := api.NewStrictHandler(impl, nil)
	m := http.NewServeMux()
	h := api.HandlerWithOptions(si, api.StdHTTPServerOptions{
		BaseURL:    BasePath,
		BaseRouter: m,
	})
	validationMiddleware := apiValidator.OapiRequestValidator(swagger)
	wrapped := httpLoggingMiddleware(validationMiddleware(h))
	return wrapped
}

func InitDbPool() *pgxpool.Pool {
	ctx := context.Background()

	pgxConfig, err := pgxpool.ParseConfig(ConnectionString)
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

	return dbpool
}

func RunMigrateScripts() {
	migrationConnectionString := ConnectionString
	// golang-migrate currently needs SSL disabled; pgx is fine with it
	if !strings.Contains(migrationConnectionString, "?") {
		migrationConnectionString += "?sslmode=disable"
	}
	m, err := migrate.New(MigrationsFolder, migrationConnectionString)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Migrate up
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		fmt.Println(err)
		return
	}
}
