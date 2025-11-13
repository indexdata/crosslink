package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

var basePath = "/patron_requests"

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = "ISIL:DK-{tenant}"
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	ctx, cancel := context.WithCancel(context.Background())
	_, _, _ = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestCrud(t *testing.T) {
	// POST
	landingId := "l1"
	borrowingId := "b1"
	requester := "r1"
	illMessage := "{\"request\": {}}"
	newPr := proapi.CreatePatronRequest{
		ID:              uuid.NewString(),
		Timestamp:       time.Now(),
		LendingPeerId:   &landingId,
		BorrowingPeerId: &borrowingId,
		Requester:       &requester,
		IllRequest:      &illMessage,
	}
	newPrBytes, err := json.Marshal(newPr)
	assert.NoError(t, err, "failed to marshal patron request")

	respBytes := httpRequest(t, "POST", basePath, newPrBytes, 201)

	var foundPr proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Equal(t, newPr.ID, foundPr.ID)
	assert.True(t, foundPr.State != "")
	assert.Equal(t, prservice.SideBorrowing, foundPr.Side)
	assert.Equal(t, newPr.Timestamp.YearDay(), foundPr.Timestamp.YearDay())
	assert.Equal(t, *newPr.LendingPeerId, *foundPr.LendingPeerId)
	assert.Equal(t, *newPr.BorrowingPeerId, *foundPr.BorrowingPeerId)
	assert.Equal(t, *newPr.Requester, *foundPr.Requester)
	assert.Equal(t, *newPr.IllRequest, *foundPr.IllRequest)

	// GET list
	respBytes = httpRequest(t, "GET", basePath, []byte{}, 200)
	var foundPrs []proapi.PatronRequest
	err = json.Unmarshal(respBytes, &foundPrs)
	assert.NoError(t, err, "failed to unmarshal patron request")

	assert.Len(t, foundPrs, 2)
	assert.Equal(t, newPr.ID, foundPrs[1].ID)

	// GET by id
	thisPrPath := basePath + "/" + newPr.ID
	respBytes = httpRequest(t, "GET", thisPrPath, []byte{}, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, newPr.ID, foundPr.ID)

	// PUT update
	landingId = "l2"
	borrowingId = "b2"
	requester = "r2"
	updatedPr := proapi.PatronRequest{
		ID:              newPr.ID,
		State:           "accepted",
		Side:            "borrowing",
		Timestamp:       time.Now(),
		LendingPeerId:   &landingId,
		BorrowingPeerId: &borrowingId,
		Requester:       &requester,
		IllRequest:      &illMessage,
	}
	updatedPrBytes, err := json.Marshal(updatedPr)
	assert.NoError(t, err)
	respBytes = httpRequest(t, "PUT", thisPrPath, updatedPrBytes, 200)
	err = json.Unmarshal(respBytes, &foundPr)
	assert.NoError(t, err, "failed to unmarshal patron request")
	assert.Equal(t, newPr.ID, foundPr.ID)
	assert.True(t, foundPr.State != "ACCEPTED")
	assert.Equal(t, prservice.SideBorrowing, foundPr.Side)
	assert.Equal(t, newPr.Timestamp.YearDay(), foundPr.Timestamp.YearDay())
	assert.Equal(t, "l1", *foundPr.LendingPeerId)
	assert.Equal(t, "b1", *foundPr.BorrowingPeerId)
	assert.Equal(t, *updatedPr.Requester, *foundPr.Requester) // Only requester can be updated now
	assert.Equal(t, *newPr.IllRequest, *foundPr.IllRequest)

	// DELETE patron request
	httpRequest(t, "DELETE", thisPrPath, []byte{}, 204)

	// GET patron request which is deleted
	httpRequest(t, "DELETE", thisPrPath, []byte{}, 404)
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, expectStatus int) []byte {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)

	if method == "POST" || method == "PUT" {
		hreq.Header.Set("Content-Type", "application/json")
	}
	hres, err := client.Do(hreq)
	assert.NoError(t, err)
	defer hres.Body.Close()
	body, err := io.ReadAll(hres.Body)
	assert.Equal(t, expectStatus, hres.StatusCode, string(body))
	assert.NoError(t, err)
	return body
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
