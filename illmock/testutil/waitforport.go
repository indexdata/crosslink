//go:build !testutil

package testutil

import (
	"github.com/stretchr/testify/assert"
	"net"
	"testing"
	"time"
)

// WaitForPort tries to connect to the specified address until timeout.
// If the port is not open before the timeout, the test fails.
func WaitForPort(t *testing.T, address string, timeout time.Duration) {
	maxDelay := 500 * time.Millisecond
	delay := 20 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			err = conn.Close()
			assert.NoError(t, err, "failed to close connection")
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
