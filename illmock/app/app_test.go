package app

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestApp(t *testing.T) {
	var app MockApp
	app.isRequester = true
	app.isSupplier = true
	go func() {
		t.Logf("Sleeping in go routine")
		time.Sleep(1000 * time.Millisecond)
		t.Logf("Shutdown in go routine")
		err := app.Shutdown()
		if err != nil {
			t.Logf("Shutdown failed: %s", err.Error())
		}
	}()
	err := app.Run()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("app.Run error %s", err.Error())
	}
}
