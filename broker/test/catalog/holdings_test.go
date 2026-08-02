package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo
var mockPeerUrl string

var shouldFailSruRequest atomic.Bool
var useMultiSupplierSruResponse atomic.Bool

// like e2e test but using consortium lookup with zoom and a mock SRU server instead of the real GVI one, so we can simulate different responses/scenarios
func TestMain(m *testing.M) {
	ill_db.PeerRefreshInterval = 0 //force refresh for every test
	ctx := context.Background()
	app.DB_PROVISION = true
	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	gviSruResponse, err := os.ReadFile("gvi_sru_response.xml")
	test.Expect(err, "failed to read gvi response file")

	gviSruResponse3, err := os.ReadFile("gvi_sru_response_3.xml")
	test.Expect(err, "failed to read gvi response file")

	sruHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFailSruRequest.Load() {
			http.Error(w, "simulated SRU failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		if useMultiSupplierSruResponse.Load() {
			if _, err := w.Write(gviSruResponse3); err != nil {
				panic(err)
			}
		} else {
			if _, err := w.Write(gviSruResponse); err != nil {
				panic(err)
			}
		}
	})
	sruServer := httptest.NewServer(sruHandler)
	defer sruServer.Close()

	directoryBytes, err := os.ReadFile("gvi_directory.json")
	test.Expect(err, "failed to read directory file")

	mockPort := utils.Must(test.GetFreePort())
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	mockPeerUrl = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"
	brokerUrl := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"

	var directoryEntries []dirapi.Entry
	err = json.Unmarshal(directoryBytes, &directoryEntries)
	test.Expect(err, "failed to unmarshal directories")

	// patch consortium peer with SRU server URL for zoom
	entry := &directoryEntries[0]
	entry.HoldingsConfig.Zoom.Address = sruServer.URL

	// MOCK_PEER_URL is only for DIRECTORY_ADAPTER=mock, so we have to patch all peers here
	for _, entry := range directoryEntries {
		(*entry.Endpoints)[0].Address = mockPeerUrl
	}

	// marshal again to set env var
	directoryBytes, err = json.Marshal(directoryEntries)
	test.Expect(err, "failed to marshal directory entries")

	test.Expect(os.Setenv("MOCK_DIRECTORY_ENTRIES", string(directoryBytes)), "failed to set mock directory entries")
	test.Expect(os.Setenv("PEER_URL", brokerUrl), "failed to set peer URL")
	app.AVAILABILITY_ADAPTER = catalog.LookupAdapterZoom
	app.DIRECTORY_ADAPTER = "api"
	app.DIRECTORY_API_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/rsdir/entries"
	app.HOLDINGS_ADAPTER = "consortium"
	app.CONSORTIUM_SYMBOL = "ISIL:GVIC"

	apptest.StartMockApp(mockPort)
	app.ConnectionString = connStr

	app.MigrationsFolder = "file://../../migrations"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func getPgText(value string) pgtype.Text {
	return pgtype.Text{
		String: value,
		Valid:  true,
	}
}

func TestRequestRequesterNotFound(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "eacf8b17-e89a-4d70-8576-e49077f8c4e1"
	data, err := os.ReadFile("request-1.xml")
	assert.NoError(t, err, "failed to read request file")
	req, err := http.NewRequest("POST", mockPeerUrl, bytes.NewReader(data))
	assert.NoError(t, err, "failed to create request")
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	// oddly here there is no transaction and no returned error
	assert.NoError(t, err, "failed to send request to mock")
	assert.Equal(t, http.StatusOK, res.StatusCode, "handler returned wrong status code")
	_, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
	assert.Error(t, err, "does not expect to find transaction")
}

func TestRequestRequestSruServerFail(t *testing.T) {
	shouldFailSruRequest.Store(true)
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "479931e1-3e94-467c-a04e-272ac8fcc154"
	data, err := os.ReadFile("request-2.xml")
	assert.NoError(t, err, "failed to read request file")
	req, err := http.NewRequest("POST", mockPeerUrl, bytes.NewReader(data))
	assert.NoError(t, err, "failed to create request")
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.NoError(t, err, "failed to send request to mock")
	assert.Equal(t, http.StatusOK, res.StatusCode, "handler returned wrong status code")
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "Unfilled" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "Unfilled", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = ERROR, error=failed to perform lookup for query 'rec.id = \"(DE-627)1795329181\"'\n" +
		"TASK, message-requester = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

// should locate the supplier via SRU with scenario UNFILLED in note
func TestRequestRequestSruServerUnfilled(t *testing.T) {
	shouldFailSruRequest.Store(false)
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "d2ce73de-2545-4ef3-be16-bff17932579a"
	data, err := os.ReadFile("request-3.xml")
	assert.NoError(t, err, "failed to read request file")
	req, err := http.NewRequest("POST", mockPeerUrl, bytes.NewReader(data))
	assert.NoError(t, err, "failed to create request")
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.NoError(t, err, "failed to send request to mock")
	assert.Equal(t, http.StatusOK, res.StatusCode, "handler returned wrong status code")
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "Unfilled" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "Unfilled", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, check-availability = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = PROBLEM, problem=no-suppliers\n" +
		"TASK, message-requester = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

// should locate the supplier via SRU with scenario LOANED in note
func TestRequestRequestSruServerLoaned(t *testing.T) {
	shouldFailSruRequest.Store(false)
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "1ff51921-4e59-4b31-aec9-c2bc3eaae2d4"
	data, err := os.ReadFile("request-4.xml")
	assert.NoError(t, err, "failed to read request file")
	req, err := http.NewRequest("POST", mockPeerUrl, bytes.NewReader(data))
	assert.NoError(t, err, "failed to create request")
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.NoError(t, err, "failed to send request to mock")
	assert.Equal(t, http.StatusOK, res.StatusCode, "handler returned wrong status code")
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "LoanCompleted" &&
			illTrans.LastRequesterAction.String == "ShippedReturn"
	})
	assert.Equal(t, "LoanCompleted", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "ShippedReturn", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, check-availability = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

// should locate three candidate suppliers via SRU; the second selected supplier fulfills the loan with scenario LOANED in note
func TestRequestRequestSruServerLoanedMultiple(t *testing.T) {
	shouldFailSruRequest.Store(false)
	useMultiSupplierSruResponse.Store(true)
	t.Cleanup(func() {
		useMultiSupplierSruResponse.Store(false)
	})
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "11deaad0-e492-4cc7-9527-6713466cc434"
	data, err := os.ReadFile("request-5.xml")
	assert.NoError(t, err, "failed to read request file")
	req, err := http.NewRequest("POST", mockPeerUrl, bytes.NewReader(data))
	assert.NoError(t, err, "failed to create request")
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.NoError(t, err, "failed to send request to mock")
	assert.Equal(t, http.StatusOK, res.StatusCode, "handler returned wrong status code")
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "LoanCompleted" &&
			illTrans.LastRequesterAction.String == "ShippedReturn"
	})
	assert.Equal(t, "LoanCompleted", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "ShippedReturn", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, check-availability = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, check-availability = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}
