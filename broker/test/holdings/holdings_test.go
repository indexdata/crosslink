package holdings

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

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/holdings"
	"github.com/indexdata/crosslink/broker/ill_db"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/directory"
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

var shouldFailSruRequest atomic.Bool

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
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	gviSruResponse, err := os.ReadFile("gvi_sru_response.xml")
	test.Expect(err, "failed to read gvi response file")

	sruHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFailSruRequest.Load() {
			http.Error(w, "simulated SRU failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte(gviSruResponse)
		w.Write(output)
	})
	sruServer := httptest.NewServer(sruHandler)
	defer sruServer.Close()

	directoryBytes, err := os.ReadFile("gvi_directory.json")
	test.Expect(err, "failed to read directory file")

	var directoryEntries []directory.Entry
	err = json.Unmarshal(directoryBytes, &directoryEntries)
	test.Expect(err, "failed to unmarshal directories")
	entry := &directoryEntries[0]
	(*entry.Endpoints)[0].Address = "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	entry.HoldingsConfig.Zoom.Address = sruServer.URL

	directoryBytes, err = json.Marshal(directoryEntries)
	test.Expect(err, "failed to marshal directory entries")

	mockPort := utils.Must(test.GetFreePort())
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("MOCK_DIRECTORY_ENTRIES", string(directoryBytes)), "failed to set mock directory entries")
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)+"/iso18626"), "failed to set peer URL")
	app.AVAILABILITY_ADAPTER = holdings.AvailabilityAdapterZoom
	app.DIRECTORY_ADAPTER = "api"
	app.DIRECTORY_API_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/directory/entries"
	app.HOLDINGS_ADAPTER = "consortium"

	apptest.StartMockApp(mockPort)
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	adapter.MOCK_PEER_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
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
	data, _ := os.ReadFile("request-1.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_PEER_URL, bytes.NewReader(data))
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
	data, _ := os.ReadFile("request-2.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_PEER_URL, bytes.NewReader(data))
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
		return illTrans.LastSupplierStatus.String == "" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = ERROR, error=failed to locate holdings for query 'rec.id = \"LOANED\"'\n" +
		"TASK, message-requester = ERROR, error=failed to send ISO18626 message\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

func TestRequestRequestSruServerOK(t *testing.T) {
	shouldFailSruRequest.Store(false)
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "d2ce73de-2545-4ef3-be16-bff17932579a"
	data, _ := os.ReadFile("request-3.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_PEER_URL, bytes.NewReader(data))
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
		return illTrans.LastSupplierStatus.String == "" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = PROBLEM, problem=no-suppliers\n" +
		"TASK, message-requester = ERROR, error=failed to send ISO18626 message\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}
