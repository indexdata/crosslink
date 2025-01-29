package app

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseConfig(t *testing.T) {
	os.Setenv("REQUESTER_SUPPLY_IDS", "Some")
	os.Setenv("HTTP_PORT", "8082")
	os.Setenv("PEER_URL", "https://localhost:8082")
	var app MockApp
	app.parseConfig()
	assert.Equal(t, "8082", app.httpPort)
	assert.Equal(t, "https://localhost:8082", app.peerUrl)
	assert.ElementsMatch(t, []string{"Some"}, app.requester.supplyingAgencyIds)
}

// TODO: Get dynamic free port
func dynamicPort() string {
	return "8081"
}

func TestWillSupplyLoaned(t *testing.T) {
	var app MockApp
	dynPort := dynamicPort()
	app.httpPort = dynPort
	app.peerUrl = "http://localhost:" + dynPort
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
	dynPort := dynamicPort()
	app.httpPort = dynPort
	app.peerUrl = "http://localhost:" + dynPort
	isoUrl := "http://localhost:" + dynPort + "/iso18626"
	go func() {
		err := app.Run()
		if err != nil {
			t.Logf("app.Run failed: %s", err.Error())
		}
	}()
	time.Sleep(5 * time.Millisecond) // wait for app to serve

	resp, err := http.Get(isoUrl)
	assert.Nil(t, err)
	assert.Equal(t, 405, resp.StatusCode)

	resp, err = http.Post(isoUrl, "text/plain", strings.NewReader("hello"))
	assert.Nil(t, err)
	assert.Equal(t, 415, resp.StatusCode)

	resp, err = http.Post(isoUrl, "text/xml", strings.NewReader("<badxml"))
	assert.Nil(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	resp, err = http.Post(isoUrl, "text/xml", strings.NewReader(
		`<ISO18626Message ill:version="1.2">
		<requestingAgencyMessageConfirmation/></ISO18626Message>`))
	assert.Nil(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	err = app.Shutdown()
	assert.Nil(t, err)
}
