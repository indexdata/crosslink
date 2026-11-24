package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
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

// ListenLocal reserves a loopback TCP port until the listener is closed.
// Pass the listener directly to the server rather than closing and rebinding it.
func ListenLocal() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// ListenerPort returns the port of a TCP listener created by ListenLocal.
func ListenerPort(listener net.Listener) int {
	return listener.Addr().(*net.TCPAddr).Port
}

// GetFreePort returns an unused port without reserving it.
// Do not use it to allocate server ports; use ListenLocal instead.
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	if addr, ok := l.Addr().(*net.TCPAddr); !ok {
		return 0, fmt.Errorf("address is not a *net.TCPAddr: %v", l.Addr())
	} else {
		return addr.Port, nil
	}
}

func WaitForServiceUp(port int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !WaitForPredicateToBeTrue(func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+strconv.Itoa(port)+"/healthz", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}) {
		panic("failed to start broker")
	} else {
		fmt.Println("Service up")
	}
}

// TerminatePGContainer stops a Postgres test container. It silently ignores the
// conflict error (HTTP 409 "removal already in progress") that arises when
// testcontainers' Ryuk reaper races with the explicit Terminate call as the
// test process winds down.
func TerminatePGContainer(ctx context.Context, pgContainer testcontainers.Container) error {
	if err := pgContainer.Terminate(ctx); err != nil {
		if cerrdefs.IsConflict(err) && strings.Contains(err.Error(), "already in progress") {
			return nil
		}
		return fmt.Errorf("failed to stop db container: %w", err)
	}
	return nil
}
