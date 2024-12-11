package main

import (
	"context"
	"fmt"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"net"
	"testing"
	"time"
)

func TestStartProcess(t *testing.T) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate pgContainer: %s", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	assert.NoError(t, err)

	ConnectionString = connStr
	MigrationsFolder = "file://../../migrations"
	HTTP_PORT = 19081

	go main()
	time.Sleep(1 * time.Second)
	listener, _ := net.Listen("tcp", fmt.Sprintf(":%d", HTTP_PORT))
	if listener == nil {
		// Port is taken by main
		fmt.Printf("Port %d is taken\n", HTTP_PORT)
	} else {
		listener.Close()
		t.Fatal("Can't start server")
	}
}
