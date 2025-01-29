package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWillSupplyLoaned(t *testing.T) {
	var app MockApp
	app.requester = &Requester{supplyingAgencyIds: []string{"WILLSUPPLY_LOANED", "WILLSUPPLY_UNFILLED", "UNFILLED", "LOANED"}}
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

func TestBadMethod(t *testing.T) {
	var app MockApp
	go func() {
		err := app.Run()
		if err != nil {
			t.Logf("app.Run failed: %s", err.Error())
		}
	}()
	time.Sleep(5 * time.Millisecond) // wait for app to serve

	resp, err := http.Get("http://localhost:8081/iso18626")
	assert.Nil(t, err)
	assert.Equal(t, 405, resp.StatusCode)

	resp, err = http.Post("http://localhost:8081/iso18626", "text/plain", strings.NewReader("hello"))
	assert.Nil(t, err)
	assert.Equal(t, 415, resp.StatusCode)

	resp, err = http.Post("http://localhost:8081/iso18626", "text/xml", strings.NewReader("<badxml"))
	assert.Nil(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	resp, err = http.Post("http://localhost:8081/iso18626", "text/xml", strings.NewReader(
		`<ISO18626Message ill:version="1.2">
		<requestingAgencyMessageConfirmation/></ISO18626Message>`))
	assert.Nil(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	err = app.Shutdown()
	assert.Nil(t, err)
}
