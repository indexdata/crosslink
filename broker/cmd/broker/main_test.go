package main

import (
	"context"
	"fmt"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/indexdata/go-utils/utils"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"net"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	go main()
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestStartProcess(t *testing.T) {
	listener, _ := net.Listen("tcp", fmt.Sprintf(":%d", app.HTTP_PORT))
	if listener == nil {
		// Port is taken by main
		fmt.Printf("Port %d is taken\n", app.HTTP_PORT)
	} else {
		listener.Close()
		t.Fatal("Can't start server")
	}
}
