package main

import (
	"context"
	"net/http"
	"testing"
	"time"

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
	time.Sleep(10 * time.Millisecond)

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
