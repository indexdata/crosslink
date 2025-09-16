package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/indexdata/crosslink/illmock/testutil"
	"github.com/magiconair/properties/assert"
)

func TestMainExit(t *testing.T) {
	// start a server on same default listening port as illmock program
	server := &http.Server{Addr: ":8081"}
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			// some process already binds to the port, fine!
			return
		}
	}()
	testutil.WaitForPort(t, "localhost:8081", time.Second)

	// Save the original exit function from main
	oldExit := exit
	defer func() { exit = oldExit }()

	var exitCode int
	exit = func(code int) {
		exitCode = code
	}
	main()
	assert.Equal(t, exitCode, 1)

	err := server.Shutdown(context.Background())
	assert.Equal(t, err, nil)
}
