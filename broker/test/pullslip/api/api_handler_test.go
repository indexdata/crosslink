package psapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	psoapi "github.com/indexdata/crosslink/broker/pullslip/oapi"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var basePath = "/pullslips"
var prRepo pr_db.PrRepo

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = ""
	ctx := context.Background()
	app.DB_PROVISION = true

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
	mockPort := utils.Must(test.GetFreePort())
	localAddress := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	test.Expect(os.Setenv("PEER_URL", localAddress), "failed to set peer URL")

	adapter.MOCK_CLIENT_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	apptest.StartMockApp(mockPort)

	ctx, cancel := context.WithCancel(context.Background())
	_, _, _, prRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestCreateSinglePullSlip(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	supSymbol := "ISIL:SUP1"
	id := "SUP1-1"
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: id,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Side: prservice.SideLending,
		RequesterSymbol: pgtype.Text{
			String: "ISIL:REQ",
			Valid:  true,
		},
		SupplierSymbol: pgtype.Text{
			String: supSymbol,
			Valid:  true,
		},
		State:    prservice.LenderStateWillSupply,
		Language: "english",
		RequesterReqID: pgtype.Text{
			String: id,
			Valid:  true,
		},
		Patron: pgtype.Text{
			String: "P456",
			Valid:  true,
		},
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{
				Title:  "Do Androids Dream of Electric Sheep?",
				Author: "Ray Bradbury",
			},
			PatronInfo: &iso18626.PatronInfo{
				GivenName: "John",
				Surname:   "Doe",
				PatronId:  "PP-789",
			},
		},
		Items:         []pr_db.PrItem{},
		TerminalState: false,
	})
	assert.NoError(t, err)

	// Create pull slip
	createPullSlip := psoapi.CreatePullSlip{
		IllTransactionId: id,
	}
	supQueryParams := "?symbol=" + supSymbol
	createPullSlipBytes, err := json.Marshal(createPullSlip)
	assert.NoError(t, err, "failed to marshal createPullSlip")
	resp, pdfBytes := httpRequest(t, "POST", basePath+supQueryParams, createPullSlipBytes, 200)
	assert.True(t, len(pdfBytes) > 100)
	loc := resp.Header.Get("Location")
	assert.True(t, strings.Contains(loc, basePath))
	assert.True(t, strings.Contains(loc, "/pdf"))

	// Check pull slip
	pullSlipUrl := basePath + strings.Split(strings.ReplaceAll(loc, "/pdf", ""), basePath)[1]
	_, respBytes := httpRequest(t, "GET", pullSlipUrl+supQueryParams, []byte{}, 200)
	var pullSlip psoapi.PullSlip
	err = json.Unmarshal(respBytes, &pullSlip)
	assert.NoError(t, err, "failed to unmarshal pull slip")
	assert.Equal(t, psoapi.Single, pullSlip.Type)
	assert.NotNil(t, pullSlip.PdfLink)

	// Check pull slip pdf
	_, respBytes = httpRequest(t, "GET", pullSlipUrl+"/pdf"+supQueryParams, []byte{}, 200)
	assert.Equal(t, pdfBytes, respBytes)
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, expectStatus int) (*http.Response, []byte) {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)
	hreq.Header.Set("X-Okapi-Tenant", "testlib")

	if method == "POST" || method == "PUT" {
		hreq.Header.Set("Content-Type", "application/json")
	}
	hres, err := client.Do(hreq)
	assert.NoError(t, err)
	defer hres.Body.Close()
	body, err := io.ReadAll(hres.Body)
	assert.Equal(t, expectStatus, hres.StatusCode, string(body))
	assert.NoError(t, err)
	return hres, body
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
