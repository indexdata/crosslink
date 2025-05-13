package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func GetNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}

func WaitForPredicateToBeTrue(predicate func() bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond) // Check every 20ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if predicate() {
				return true
			}
		}
	}
}

func Expect(err error, message string) {
	if err != nil {
		panic(fmt.Sprintf(message+" Errror : %s", err))
	}
}

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	// release for now so it can be bound by the actual server
	// a more robust solution would be to bind the server to the port and close it here
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func WaitForServiceUp(port int) {
	if !WaitForPredicateToBeTrue(func() bool {
		resp, err := http.Get("http://localhost:" + strconv.Itoa(port) + "/healthz")
		if err != nil {
			return false
		}
		return resp.StatusCode == http.StatusOK
	}) {
		panic("failed to start broker")
	} else {
		fmt.Println("Service up")
	}
}

func StartPGContainer() (context.Context, *postgres.PostgresContainer, string, error) {
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
		return ctx, pgContainer, "", fmt.Errorf("failed to start db container: %w", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return ctx, pgContainer, "", fmt.Errorf("failed to get conn string: %w", err)
	}
	return ctx, pgContainer, connStr, nil
}

func TerminatePGContainer(ctx context.Context, pgContainer testcontainers.Container) error {
	if err := pgContainer.Terminate(ctx); err != nil {
		return fmt.Errorf("failed to stop db container: %w", err)
	}
	return nil
}
