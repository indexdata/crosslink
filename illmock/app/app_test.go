package app

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseConfig(t *testing.T) {
	os.Setenv("AGENCY_SCENARIO", "Some")
	os.Setenv("HTTP_PORT", "8082")
	os.Setenv("PEER_URL", "https://localhost:8082")
	os.Setenv("AGENCY_TYPE", "ABC")
	os.Setenv("SUPPLYING_AGENCY_ID", "S1")
	os.Setenv("REQUESTING_AGENCY_ID", "R1")
	var app MockApp
	app.parseConfig()
	assert.Equal(t, "8082", app.httpPort)
	assert.Equal(t, "ABC", app.agencyType)
	assert.Equal(t, "S1", app.requester.supplyingAgencyId)
	assert.Equal(t, "R1", app.requester.requestingAgencyId)
	assert.Equal(t, "https://localhost:8082", app.peerUrl)
	assert.ElementsMatch(t, []string{"Some"}, app.requester.agencyScenario)
}

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	// release for now so it can be bound by the actual server
	// a more robust solution would be to bind the server to the port and close it here
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// getFreePortTest returns a free port as a string for testing.
func getFreePortTest(t *testing.T) string {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get a free port: %v", err)
	}
	return strconv.Itoa(port)
}

func TestWillSupplyLoaned(t *testing.T) {
	var app MockApp
	dynPort := getFreePortTest(t)
	app.httpPort = dynPort
	app.peerUrl = "http://localhost:" + dynPort
	app.requester.agencyScenario = []string{"WILLSUPPLY_LOANED", "WILLSUPPLY_UNFILLED", "UNFILLED", "LOANED"}
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
	dynPort := getFreePortTest(t)
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
