package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"indexdata/directoryish/app"
)

var dbpool *pgxpool.Pool
var handler http.Handler

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("directoryish"),
		postgres.WithUsername("directoryish"),
		postgres.WithPassword("directoryish"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to start db container: %s", err))
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("failed to get conn string: %s", err))
	}

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../migrations"

	time.Sleep(1 * time.Second)

	app.RunMigrateScripts()
	dbpool = app.InitDbPool()
	defer dbpool.Close()
	handler = app.InitHandler(ctx, dbpool)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
	}
	os.Exit(code)
}

func TestEmptyGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/entries", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if string(data) != "[]\n" {
		t.Errorf("expected [] got %v", string(data))
	}
}
