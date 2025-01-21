package app

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestWillSupplyLoaned(t *testing.T) {
	var app MockApp
	app.isRequester = true
	app.isSupplier = true
	app.supplyingAgencyId = "WILLSUPPLY_LOANED"
	go func() {
		time.Sleep(1000 * time.Millisecond)
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

func TestWillSupplyUnfilled(t *testing.T) {
	var app MockApp
	app.isRequester = true
	app.isSupplier = true
	app.supplyingAgencyId = "WILLSUPPLY_UNFILLED"
	go func() {
		time.Sleep(1000 * time.Millisecond)
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
