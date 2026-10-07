package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/app"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/testutil"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true
	pgContainer, err := testutil.RunPostgres(ctx)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"

	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func runArchive(t *testing.T) error {
	t.Helper()
	flags, args := flag.CommandLine, os.Args
	t.Cleanup(func() {
		flag.CommandLine, os.Args = flags, args
	})
	flag.CommandLine = flag.NewFlagSet("archive", flag.ContinueOnError)
	os.Args = []string{"archive"}
	return run()
}

func TestArchiveWithoutDirectory(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	unavailableURL := server.URL + "/directory/entries"
	server.Close()
	for _, source := range []string{"invalid Directory URL", unavailableURL} {
		t.Run(source, func(t *testing.T) {
			original := app.DIRECTORY_API_URL
			t.Cleanup(func() { app.DIRECTORY_API_URL = original })
			app.DIRECTORY_API_URL = source
			assert.NoError(t, runArchive(t))
		})
	}
}

func TestArchiveDatabaseInitializationErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		provision bool
		message   string
	}{
		{"provision", true, "DB provision failed"},
		{"pool", false, "unable to create pool to database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, provision, migrate := app.ConnectionString, app.DB_PROVISION, app.DB_MIGRATE
			t.Cleanup(func() {
				app.ConnectionString, app.DB_PROVISION, app.DB_MIGRATE = conn, provision, migrate
			})
			app.ConnectionString = "postgres://%invalid"
			app.DB_PROVISION, app.DB_MIGRATE = tc.provision, false
			assert.ErrorContains(t, runArchive(t), tc.message)
		})
	}
}
