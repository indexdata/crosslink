package main

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestStartProcess(t *testing.T) {
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
