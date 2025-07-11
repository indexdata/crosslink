//go:build !testutil
// +build !testutil

package testutil

import (
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
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func GetFreePortTest(t *testing.T) string {
	return strconv.Itoa(GetFreePort(t))
}
