package app

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestWillSupplyLoaned(t *testing.T) {
	var app MockApp
	app.supplier = &Supplier{}
	app.requester = &Requester{supplyingAgencyIds: []string{"WILLSUPPLY_LOANDED", "WILLSUPPLY_UNFILLED", "UNFILLED", "LOANDED"}}
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
