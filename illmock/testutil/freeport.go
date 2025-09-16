//go:build !testutil

package testutil

import (
	"github.com/stretchr/testify/assert"
	"net"
	"strconv"
	"testing"
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
	defer func() {
		dErr := l.Close()
		assert.NoError(t, dErr)
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func GetFreePortTest(t *testing.T) string {
	return strconv.Itoa(GetFreePort(t))
}
