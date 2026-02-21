package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"net/http"
	"syscall"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/dbutil"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbutil.DB_PROVISION = true

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
	startApp(ctx)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func startApp(ctx context.Context) {
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		err := app.Run(ctx)
		test.Expect(err, "failed to start app")
	}()
	test.WaitForServiceUp(app.HTTP_PORT)
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

func TestGracefulShutdown(t *testing.T) {
	// Save original shutdown delay and restore after test
	originalDelay := app.SHUTDOWN_DELAY
	app.SHUTDOWN_DELAY = 1 * time.Second
	defer func() {
		app.SHUTDOWN_DELAY = originalDelay
	}()

	// Create channels for controlling the flow of the slow handler
	requestReceived := make(chan struct{})
	allowRequestToFinish := make(chan struct{})
	requestCompleted := make(chan struct{})

	// Inject a slow endpoint into the server's mux
	app.ServeMux.HandleFunc("GET /slow-test", func(w http.ResponseWriter, r *http.Request) {
		// Signal that the request has been received
		close(requestReceived)

		// Wait until the test allows this handler to complete
		<-allowRequestToFinish

		// Complete the request
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("Slow response completed")); err != nil {
			t.Error("Failed to write response:", err)
		}
	})

	// Start a goroutine that makes a request to our slow endpoint
	go func() {
		// Use the slow endpoint we just registered
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/slow-test", app.HTTP_PORT))
		if err == nil && resp != nil {
			resp.Body.Close()
		}
		close(requestCompleted)
	}()

	// Wait for the request to be received by the server
	select {
	case <-requestReceived:
		// Server has received the request and handler is now waiting
	case <-time.After(3 * time.Second):
		assert.Fail(t, "Server didn't receive the request within expected time")
		return
	}

	// Record port for later verification
	portToCheck := app.HTTP_PORT

	// Send SIGTERM to trigger graceful shutdown
	process, err := os.FindProcess(os.Getpid())
	assert.NoError(t, err)
	err = process.Signal(syscall.SIGTERM)
	assert.NoError(t, err, "Should be able to send signal to self")

	// Give the server a moment to start its shutdown sequence
	time.Sleep(200 * time.Millisecond)

	// Now allow the handler to complete the request
	close(allowRequestToFinish)

	// Verify the request completes
	select {
	case <-requestCompleted:
		// Request completed successfully
	case <-time.After(3 * time.Second):
		assert.Fail(t, "Request did not complete during graceful shutdown")
		return
	}

	// Wait for the server to fully shut down (SHUTDOWN_DELAY + a bit more)
	time.Sleep(2 * time.Second)

	// Try to connect to see if the server has shut down
	conn, _ := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", portToCheck), time.Second)
	if conn != nil {
		conn.Close()
		assert.Fail(t, "Server did not shut down as expected")
	}
}
