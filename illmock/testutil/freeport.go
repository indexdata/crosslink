//go:build !testutil
// +build !testutil

package testutil

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func GetFreeListener(t *testing.T) *net.TCPListener {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to resolve TCP address: ", err)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal("Failed to listen on TCP address: ", err)
	}
	return listener
}

func GetFreePort(t *testing.T) int {
	l := GetFreeListener(t)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func GetFreePortTest(t *testing.T) string {
	return strconv.Itoa(GetFreePort(t))
}

func WaitForPort(t *testing.T, address string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			conn.Close()
			return // Port is ready
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for port %s", address)
}
