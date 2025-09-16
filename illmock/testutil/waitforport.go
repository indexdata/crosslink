//go:build !testutil

package testutil

import (
	"net"
	"testing"
	"time"
)

func WaitForPort(t *testing.T, address string, timeout time.Duration) {
	maxDelay := 500 * time.Millisecond
	delay := 20 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			conn.Close()
			return // Port is ready
		}
		time.Sleep(delay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	t.Fatalf("timeout waiting for port %s", address)
}
