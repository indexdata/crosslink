package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/indexdata/crosslink/broker/app"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	hostDockerInternal := os.Getenv("HOST_DOCKER_INTERNAL")
	fmt.Printf("HOST_DOCKER_INTERNAL=%s\n", hostDockerInternal)
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			if len(hostDockerInternal) > 0 {
				hc.ExtraHosts = append(hc.ExtraHosts, fmt.Sprintf("host.docker.internal:%s", hostDockerInternal))
			}
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to start db container: %s", err))
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("failed to get conn string: %s", err))
	}

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = 19081

	go main()
	time.Sleep(1 * time.Second)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
	}
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
