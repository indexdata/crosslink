package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/vcs"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	mocks "github.com/indexdata/crosslink/broker/test/mocks"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var illRepo ill_db.IllRepo
var eventRepo events.EventRepo
var sseBroker *api.SseBroker
var mockIllRepoError = new(mocks.MockIllRepositoryError)
var mockEventRepoError = new(mocks.MockEventRepositoryError)
var handlerMock = api.NewApiHandler(mockEventRepoError, mockIllRepoError, common.NewTenant(""), api.LIMIT_DEFAULT)

func TestMain(m *testing.M) {
	app.TENANT_TO_SYMBOL = "ISIL:DK-{tenant}"
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
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	ctx, cancel := context.WithCancel(context.Background())
	appContext := apptest.StartAppReturnContext(ctx)
	illRepo = appContext.IllRepo
	eventRepo = appContext.EventRepo
	sseBroker = appContext.SseBroker
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestGetIndex(t *testing.T) {
	httpGet(t, "/", "", http.StatusOK)
	body := getResponseBody(t, "/")
	var resp oapi.Index
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, vcs.GetCommit(), resp.Revision)
	assert.Equal(t, vcs.GetSignature(), resp.Signature)
	assert.Equal(t, getLocalhostWithPort()+api.ILL_TRANSACTIONS_PATH, resp.Links.IllTransactionsLink)
	assert.Equal(t, getLocalhostWithPort()+api.EVENTS_PATH, resp.Links.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+api.PEERS_PATH, resp.Links.PeersLink)
	assert.Equal(t, getLocalhostWithPort()+api.LOCATED_SUPPLIERS_PATH, resp.Links.LocatedSuppliersLink)
}

func TestGetEvents(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameMessageRequester)

	httpGet(t, "/events", "", http.StatusBadRequest)

	body := getResponseBody(t, "/events?ill_transaction_id="+illId)
	var resp oapi.Events
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Items), 1)
	assert.GreaterOrEqual(t, resp.About.Count, int64(1))
	assert.GreaterOrEqual(t, resp.About.Count, int64(len(resp.Items)))
	assert.Equal(t, eventId, resp.Items[0].Id)

	body = getResponseBody(t, "/events?ill_transaction_id=not-exists")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 0)
	assert.Equal(t, []oapi.Event{}, resp.Items)
}

func TestGetIllTransactions(t *testing.T) {
	id := apptest.GetIllTransId(t, illRepo)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	trans, err := illRepo.GetIllTransactionById(ctx, id)
	assert.NoError(t, err)
	reqReqId := uuid.NewString()
	trans.RequesterRequestID = pgtype.Text{
		String: reqReqId,
		Valid:  true,
	}
	trans, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(trans))
	assert.NoError(t, err)
	body := getResponseBody(t, "/ill_transactions")
	var resp oapi.IllTransactions
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Items), 1)
	assert.Equal(t, resp.About.Count, int64(len(resp.Items)))
	// Query
	body = getResponseBody(t, "/ill_transactions?requester_req_id="+url.QueryEscape(reqReqId))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, reqReqId, resp.Items[0].RequesterRequestID)

	body = getResponseBody(t, "/ill_transactions?requester_req_id=not-exists")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 0)
	assert.Equal(t, []oapi.IllTransaction{}, resp.Items)

	for i := range 2 * api.LIMIT_DEFAULT {
		requester := "ISIL:DK-BIB1"
		if i > api.LIMIT_DEFAULT+3 {
			requester = "ISIL:DK-BIB2"
		}
		illId := uuid.NewString()
		reqReqId := uuid.NewString()
		_, err := illRepo.SaveIllTransaction(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
			ID: illId,
			RequesterSymbol: pgtype.Text{
				String: requester,
				Valid:  true,
			},
			RequesterRequestID: pgtype.Text{
				String: reqReqId,
				Valid:  true,
			},
			Timestamp: test.GetNow(),
		})
		assert.NoError(t, err)
	}
	body = getResponseBody(t, "/ill_transactions")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, int(api.LIMIT_DEFAULT), len(resp.Items))
	count := resp.About.Count
	assert.GreaterOrEqual(t, count, int64(1+2*api.LIMIT_DEFAULT))
	assert.LessOrEqual(t, count, int64(3*api.LIMIT_DEFAULT))
	assert.Nil(t, resp.About.PrevLink)
	assert.NotNil(t, resp.About.NextLink)
	assert.Equal(t, getLocalhostWithPort()+"/ill_transactions?offset=10", *resp.About.NextLink)

	body = getResponseBody(t, "/ill_transactions?offset=1000")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, count, resp.About.Count)

	body = getResponseBody(t, "/ill_transactions?limit=0")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, count, resp.About.Count)

	body = getResponseBody(t, "/ill_transactions?offset=3&limit="+strconv.Itoa(int(api.LIMIT_DEFAULT)))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, resp.About.Count, int64(1+2*api.LIMIT_DEFAULT))
	assert.LessOrEqual(t, resp.About.Count, int64(3*api.LIMIT_DEFAULT))
	prevLink := *resp.About.PrevLink
	assert.Contains(t, prevLink, "offset=0")

	body = getResponseBody(t, "/broker/ill_transactions?requester_symbol="+url.QueryEscape("ISIL:DK-BIB1"))
	resp.About.NextLink = nil
	resp.About.PrevLink = nil
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, int(api.LIMIT_DEFAULT), len(resp.Items))
	assert.GreaterOrEqual(t, resp.About.Count, int64(3+api.LIMIT_DEFAULT))
	assert.LessOrEqual(t, resp.About.Count, int64(2*api.LIMIT_DEFAULT))

	assert.Nil(t, resp.About.PrevLink)
	assert.NotNil(t, resp.About.NextLink)
	nextLink := *resp.About.NextLink
	assert.True(t, strings.HasPrefix(nextLink, getLocalhostWithPort()+"/broker/ill_transactions?"))
	assert.Contains(t, nextLink, "requester_symbol="+url.QueryEscape("ISIL:DK-BIB1"))
	// we have estblished that the next link is correct, now we will check if it works
	hres, err := http.Get(nextLink) // nolint:gosec
	assert.NoError(t, err)
	defer hres.Body.Close()
	body, err = io.ReadAll(hres.Body)
	assert.NoError(t, err)
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.NotNil(t, resp.About.PrevLink)
	prevLink = *resp.About.PrevLink
	assert.True(t, strings.HasPrefix(prevLink, getLocalhostWithPort()+"/broker/ill_transactions?"))
	assert.Contains(t, prevLink, "offset=0")

	body = getResponseBody(t, "/ill_transactions?cql="+url.QueryEscape("requester_symbol = ISIL:DK-BIB1"))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 10)

	body = getResponseBody(t, "/ill_transactions?cql="+url.QueryEscape("requester_symbol = ISIL:DK-BIB2"))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 6)

	body = getResponseBody(t, "/ill_transactions?cql="+url.QueryEscape("requester_symbol <> ISIL:DK-BIB2"))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 10)

	body = getResponseBody(t, "/ill_transactions?cql="+url.QueryEscape("requester_symbol = ISIL:DK-BIB3"))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 0)

	body = getResponseBody(t, "/ill_transactions?cql="+url.QueryEscape("requester_symbol = ISIL:DK-BIB3 or requester_symbol = ISIL:DK-BIB2"))
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 6)
}

func TestGetIllTransactionsId(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	body := getResponseBody(t, "/ill_transactions/"+illId)
	var resp oapi.IllTransaction
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, illId, resp.Id)
	assert.Equal(t, getLocalhostWithPort()+"/events?ill_transaction_id="+url.PathEscape(illId), resp.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/located_suppliers?ill_transaction_id="+url.PathEscape(illId), resp.LocatedSuppliersLink)

	// Delete peer
	httpRequest(t, "DELETE", "/ill_transactions/"+illId, nil, "", http.StatusNoContent)
	httpRequest(t, "DELETE", "/ill_transactions/"+illId, nil, "", http.StatusNotFound)
}

func TestGetLocatedSuppliers(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	peer := apptest.CreatePeer(t, illRepo, "ISIL:LOC_SUP", "")
	locSup := apptest.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_SUP", string(iso18626.TypeStatusLoaned))
	httpGet(t, "/located_suppliers", "", http.StatusBadRequest)
	var resp oapi.LocatedSuppliers
	body := getResponseBody(t, "/located_suppliers?ill_transaction_id="+illId)
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Items), 1)
	assert.Equal(t, resp.Items[0].Id, locSup.ID)
	assert.GreaterOrEqual(t, resp.About.Count, int64(len(resp.Items)))

	body = getResponseBody(t, "/located_suppliers?ill_transaction_id=not-exists")
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 0)
	assert.Equal(t, []oapi.LocatedSupplier{}, resp.Items)
}

func TestBrokerCRUD(t *testing.T) {
	// app.TENANT_TO_SYMBOL = "ISIL:DK-{tenant}"
	illId := uuid.New().String()
	reqReqId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID: illId,
		RequesterSymbol: pgtype.Text{
			String: "ISIL:DK-DIKU",
			Valid:  true,
		},
		RequesterRequestID: pgtype.Text{
			String: reqReqId,
			Valid:  true,
		},
		Timestamp: test.GetNow(),
	})
	assert.NoError(t, err)

	body := httpGet(t, "/broker/ill_transactions/"+illId, "diku", http.StatusOK)
	var tran oapi.IllTransaction
	err = json.Unmarshal(body, &tran)
	assert.NoError(t, err)
	assert.Equal(t, illId, tran.Id)
	assert.Equal(t, getLocalhostWithPort()+"/broker/events?ill_transaction_id="+url.PathEscape(illId), tran.EventsLink)
	assert.Equal(t, getLocalhostWithPort()+"/broker/located_suppliers?ill_transaction_id="+url.PathEscape(illId), tran.LocatedSuppliersLink)

	httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "diku", http.StatusOK)
	httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "ruc", http.StatusNotFound)
	httpGet(t, "/broker/ill_transactions/"+illId, "ruc", http.StatusNotFound)
	httpGet(t, "/broker/ill_transactions/"+illId, "", http.StatusNotFound)

	body = httpGet(t, "/broker/ill_transactions/"+illId+"?requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "", http.StatusOK)
	err = json.Unmarshal(body, &tran)
	assert.NoError(t, err)
	assert.Equal(t, illId, tran.Id)

	assert.Equal(t, 1, len(httpGetTrans(t, "/broker/ill_transactions", "diku", http.StatusOK)))

	assert.Equal(t, 1, len(httpGetTrans(t, "/broker/ill_transactions?cql="+url.QueryEscape("requester_symbol=ISIL:DK-DIKU"), "diku", http.StatusOK)))

	assert.Equal(t, 0, len(httpGetTrans(t, "/broker/ill_transactions?cql="+url.QueryEscape("requester_symbol=ISIL:DK-RUC"), "diku", http.StatusOK)))

	assert.Equal(t, 0, len(httpGetTrans(t, "/broker/ill_transactions", "ruc", http.StatusOK)))

	assert.Equal(t, 0, len(httpGetTrans(t, "/broker/ill_transactions", "", http.StatusOK)))

	body = httpGet(t, "/broker/ill_transactions?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var resp oapi.IllTransactions
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, illId, resp.Items[0].Id)

	peer := apptest.CreatePeer(t, illRepo, "ISIL:LOC_OTHER", "")
	locSup := apptest.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_OTHER", string(iso18626.TypeStatusLoaned))

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var supps oapi.LocatedSuppliers
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 1)
	assert.Equal(t, locSup.ID, supps.Items[0].Id)

	body = httpGet(t, "/broker/located_suppliers?ill_transaction_id="+url.QueryEscape(illId), "diku", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 1)
	assert.Equal(t, locSup.ID, supps.Items[0].Id)

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(reqReqId), "ruc", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 0)

	body = httpGet(t, "/broker/located_suppliers?requester_req_id="+url.QueryEscape(uuid.NewString()), "diku", http.StatusOK)
	err = json.Unmarshal(body, &supps)
	assert.NoError(t, err)
	assert.Len(t, supps.Items, 0)

	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameMessageRequester)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId), "diku", http.StatusOK)
	var events oapi.Events
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].Id)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId)+"&requester_symbol="+url.QueryEscape("ISIL:DK-DIKU"), "", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].Id)

	body = httpGet(t, "/broker/events?ill_transaction_id="+url.QueryEscape(illId), "diku", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 1)
	assert.Equal(t, eventId, events.Items[0].Id)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(reqReqId), "ruc", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 0)

	body = httpGet(t, "/broker/events?requester_req_id="+url.QueryEscape(uuid.NewString()), "diku", http.StatusOK)
	err = json.Unmarshal(body, &events)
	assert.NoError(t, err)
	assert.Len(t, events.Items, 0)
}

func TestPeersLinks(t *testing.T) {
	for i := 0; i < 2*int(api.LIMIT_DEFAULT); i++ {
		peer := "ISIL:DK-PEER" + strconv.Itoa(i)
		toCreate := oapi.Peer{
			Id:            uuid.New().String(),
			Name:          peer,
			Url:           "https://url.com",
			Symbols:       []string{peer},
			RefreshPolicy: oapi.Transaction,
		}
		apptest.CreatePeer(t, illRepo, toCreate.Symbols[0], "")
	}
	resp := getPeers(t)
	assert.Len(t, resp.Items, int(api.LIMIT_DEFAULT))
	assert.GreaterOrEqual(t, int(resp.About.Count), int(2*api.LIMIT_DEFAULT))
	assert.NotNil(t, resp.About.NextLink)
	assert.True(t, strings.HasPrefix(*resp.About.NextLink, getLocalhostWithPort()+"/peers?"))
	assert.Contains(t, *resp.About.NextLink, "offset="+strconv.Itoa(int(api.LIMIT_DEFAULT)))
	assert.Nil(t, resp.About.PrevLink)

	body := getResponseBody(t, "/peers?offset="+strconv.Itoa(int(api.LIMIT_DEFAULT)-1))
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.NotNil(t, resp.About.PrevLink)
	assert.Contains(t, *resp.About.PrevLink, "offset=0")
	assert.NotNil(t, resp.About.NextLink)
}

func TestPeersNoHeaders(t *testing.T) {
	// Create peer
	toCreate := oapi.Peer{
		// No ID
		Name:          "Peer",
		Url:           "https://url.com",
		Symbols:       []string{"ISIL:PEER"},
		RefreshPolicy: oapi.Transaction,
	}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err)
	body := httpRequest(t, "POST", "/peers", jsonBytes, "", http.StatusCreated)
	var respPeer oapi.Peer
	err = json.Unmarshal(body, &respPeer)
	assert.NoError(t, err)

	// Delete peer
	httpRequest(t, "DELETE", "/peers/"+respPeer.Id, nil, "", http.StatusNoContent)
	httpRequest(t, "DELETE", "/peers/"+respPeer.Id, nil, "", http.StatusNotFound)
}

func TestPeersCRUD(t *testing.T) {
	headers := map[string]string{
		"X-Okapi-Tenant": "diku",
		"X-Okapi-Url":    "http://localhost:1234",
	}
	custom := map[string]interface{}{
		"name":  "v1",
		"email": "v2",
		"lost":  "value",
	}
	// Create peer
	loanCount := int32(5)
	borrowCount := int32(10)
	toCreate := oapi.Peer{
		Id:            uuid.New().String(),
		Name:          "Peer",
		Url:           "https://url.com",
		Symbols:       []string{"ISIL:PEER"},
		RefreshPolicy: oapi.Transaction,
		CustomData:    &custom,
		HttpHeaders:   &headers,
		BranchSymbols: &[]string{"ISIL:PEER-Branch"},
		LoansCount:    &loanCount,
		BorrowsCount:  &borrowCount,
	}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err)
	body := httpRequest(t, "POST", "/peers", jsonBytes, "", http.StatusCreated)
	var respPeer oapi.Peer
	err = json.Unmarshal(body, &respPeer)
	assert.NoError(t, err)
	assert.Equal(t, toCreate.Id, respPeer.Id)
	assert.Equal(t, "diku", (*toCreate.HttpHeaders)["X-Okapi-Tenant"])

	var respPeers oapi.Peers
	// Query the just POSTed peer
	body = getResponseBody(t, "/peers?cql="+url.QueryEscape("symbol any ISIL:PEER"))
	err = json.Unmarshal(body, &respPeers)
	assert.NoError(t, err)
	assert.Equal(t, toCreate.Id, respPeers.Items[0].Id)
	assert.GreaterOrEqual(t, len(respPeers.Items), 1)
	assert.Equal(t, "Peer", respPeers.Items[0].Name)
	assert.Equal(t, "ISIL:PEER", respPeers.Items[0].Symbols[0])
	assert.Equal(t, "https://url.com", respPeers.Items[0].Url)
	assert.Equal(t, oapi.Opaque, respPeers.Items[0].BrokerMode)
	assert.Equal(t, "Unknown", respPeers.Items[0].Vendor)
	assert.Equal(t, "v1", (*respPeers.Items[0].CustomData)["name"])
	assert.Equal(t, "v2", (*respPeers.Items[0].CustomData)["email"])
	assert.Nil(t, (*respPeers.Items[0].CustomData)["lost"])
	assert.Equal(t, "http://localhost:1234", (*respPeers.Items[0].HttpHeaders)["X-Okapi-Url"])
	assert.Equal(t, int32(5), *respPeers.Items[0].LoansCount)
	assert.Equal(t, int32(10), *respPeers.Items[0].BorrowsCount)

	// Cannot post same again
	httpRequest(t, "POST", "/peers", jsonBytes, "", http.StatusBadRequest)

	// Update peer
	toCreate.Name = "Updated"
	toCreate.Symbols = append(toCreate.Symbols, "ISIL:UPDATED")
	branchSymbols := []string{}
	if toCreate.BranchSymbols != nil {
		branchSymbols = *toCreate.BranchSymbols
	}
	branchSymbols = append(branchSymbols, "ISIL:UPDATED-Branch")
	toCreate.BranchSymbols = &branchSymbols
	toCreate.Url = "https://url2.com"
	toCreate.BrokerMode = oapi.Transparent
	toCreate.Vendor = "Known"
	updLoanCount := int32(10)
	toCreate.LoansCount = &updLoanCount
	updBorrowCount := int32(15)
	toCreate.BorrowsCount = &updBorrowCount

	jsonBytes, err = json.Marshal(toCreate)
	assert.NoError(t, err)
	body = httpRequest(t, "PUT", "/peers/"+toCreate.Id, jsonBytes, "", http.StatusOK)

	err = json.Unmarshal(body, &respPeer)
	assert.NoError(t, err)
	assert.Equal(t, toCreate.Id, respPeer.Id)
	assert.Equal(t, "Updated", respPeer.Name)
	assert.Len(t, respPeer.Symbols, 2)
	assert.Equal(t, 2, len(*respPeer.BranchSymbols))
	assert.Equal(t, "https://url2.com", respPeer.Url)
	assert.Equal(t, oapi.Transparent, respPeer.BrokerMode)
	assert.Equal(t, "Known", respPeer.Vendor)
	assert.Equal(t, int32(10), *respPeer.LoansCount)
	assert.Equal(t, int32(15), *respPeer.BorrowsCount)
	// Get peer
	respPeer = getPeerById(t, toCreate.Id)
	assert.Equal(t, toCreate.Id, respPeer.Id)
	assert.Equal(t, "Updated", respPeer.Name)
	assert.Equal(t, int32(10), *respPeer.LoansCount)
	assert.Equal(t, int32(15), *respPeer.BorrowsCount)
	// Get peers
	respPeers = getPeers(t)
	assert.GreaterOrEqual(t, len(respPeers.Items), 1)

	body = getResponseBody(t, "/peers?offset=0&limit=1")
	err = json.Unmarshal(body, &respPeers)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, respPeers.About.Count, int64(1))

	body = getResponseBody(t, "/peers?limit=0")
	err = json.Unmarshal(body, &respPeers)
	assert.NoError(t, err)
	assert.Equal(t, []oapi.Peer{}, respPeers.Items)

	httpGet(t, "/peers?cql="+url.QueryEscape("badfield any ISIL:PEER"), "", http.StatusBadRequest)

	httpGet(t, "/peers?cql="+url.QueryEscape("("), "", http.StatusBadRequest)

	// Query peers
	body = getResponseBody(t, "/peers?cql="+url.QueryEscape("symbol any ISIL:PEER"))
	err = json.Unmarshal(body, &respPeers)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(respPeers.Items), 1)
	assert.Equal(t, toCreate.Id, respPeers.Items[0].Id)
	assert.NotNil(t, respPeers.Items[0].CustomData)
	assert.Equal(t, "v1", (*respPeers.Items[0].CustomData)["name"])
	assert.Equal(t, "v2", (*respPeers.Items[0].CustomData)["email"])
	assert.Equal(t, "http://localhost:1234", (*respPeers.Items[0].HttpHeaders)["X-Okapi-Url"])

	// Delete peer
	httpRequest(t, "DELETE", "/peers/"+toCreate.Id, nil, "", http.StatusNoContent)
	httpRequest(t, "DELETE", "/peers/"+toCreate.Id, nil, "", http.StatusNotFound)

	// Check no peers left
	respPeers = getPeers(t)
	for _, p := range respPeers.Items {
		assert.NotEqual(t, toCreate.Id, p.Id)
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		method   string
	}{
		{
			name:     "peers",
			endpoint: "/peers/not_found",
			method:   "GET",
		},
		{
			name:     "illTransaction",
			endpoint: "/ill_transactions/not_found",
			method:   "GET",
		},
		{
			name:     "peersPut",
			endpoint: "/peers/not_found",
			method:   "PUT",
		},
		{
			name:     "peersDelete",
			endpoint: "/peers/not_found",
			method:   "DELETE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRequest(t, tt.method, tt.endpoint, nil, "", http.StatusNotFound)
		})
	}
}
func TestGetEventsDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	reqId := uuid.New().String()
	handlerMock.GetEvents(rr, req, oapi.GetEventsParams{RequesterReqId: &reqId})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetIllTransactionsDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetIllTransactions(rr, req, oapi.GetIllTransactionsParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetIllTransactionsIdDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetIllTransactionsId(rr, req, "id", oapi.GetIllTransactionsIdParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetPeersDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetPeers(rr, req, oapi.GetPeersParams{})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestPostPeersDbError(t *testing.T) {
	toCreate := oapi.Peer{
		Id:            uuid.New().String(),
		Name:          "Peer",
		Url:           "https://url.com",
		Symbols:       []string{"ISIL:PEER"},
		RefreshPolicy: oapi.Transaction,
	}
	jsonBytes, err := json.Marshal(toCreate)
	if err != nil {
		t.Errorf("Error marshaling JSON: %s", err)
	}
	req, _ := http.NewRequest("GET", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handlerMock.PostPeers(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostPeersError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", bytes.NewBuffer([]byte{}))
	rr := httptest.NewRecorder()
	handlerMock.PostPeers(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestDeletePeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.DeletePeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetPeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.GetPeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestPutPeersSymbolDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handlerMock.PutPeersId(rr, req, "s")
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}
func TestGetLocatedSuppliersDbError(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	reqReqId := uuid.New().String()
	handlerMock.GetLocatedSuppliers(rr, req, oapi.GetLocatedSuppliersParams{RequesterReqId: &reqReqId})
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostArchiveIllTransactions(t *testing.T) {
	// Loan Completed
	minus5days := pgtype.Timestamp{
		Time:  time.Now().Add(-(24 * 5 * time.Hour)),
		Valid: true,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	illId := apptest.GetIllTransId(t, illRepo)
	illTr, err := illRepo.GetIllTransactionById(ctx, illId)
	assert.Nil(t, err)
	illTr.LastSupplierStatus = apptest.CreatePgText(string(iso18626.TypeStatusLoanCompleted))
	illTr.Timestamp = minus5days
	_, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTr))
	assert.Nil(t, err)
	peer := apptest.CreatePeer(t, illRepo, "ISIL:LOC_SUP", "")
	apptest.CreateLocatedSupplier(t, illRepo, illId, peer.ID, "ISIL:LOC_SUP", string(iso18626.TypeStatusLoaned))
	apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess, events.EventNameSelectSupplier)

	// Unfilled
	illId2 := apptest.GetIllTransId(t, illRepo)
	illTr, err = illRepo.GetIllTransactionById(ctx, illId2)
	assert.Nil(t, err)
	illTr.LastSupplierStatus = apptest.CreatePgText(string(iso18626.TypeStatusUnfilled))
	illTr.Timestamp = minus5days
	_, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTr))
	assert.Nil(t, err)

	// Loaned
	illId3 := apptest.GetIllTransId(t, illRepo)
	illTr, err = illRepo.GetIllTransactionById(ctx, illId3)
	assert.Nil(t, err)
	illTr.LastSupplierStatus = apptest.CreatePgText(string(iso18626.TypeStatusLoaned))
	illTr.Timestamp = minus5days
	_, err = illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTr))
	assert.Nil(t, err)

	body := httpRequest(t, "POST", "/archive_ill_transactions?archive_delay=1d&archive_status=LoanCompleted,CopyCompleted,Unfilled", nil, "", http.StatusOK)
	var resp oapi.StatusMessage
	err = json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, "Archive process started", resp.Status)

	test.WaitForPredicateToBeTrue(func() bool {
		_, err = illRepo.GetIllTransactionById(ctx, illId)
		return errors.Is(err, pgx.ErrNoRows)
	})

	_, err = illRepo.GetIllTransactionById(ctx, illId)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))

	_, err = illRepo.GetIllTransactionById(ctx, illId2)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))

	illTr, err = illRepo.GetIllTransactionById(ctx, illId3)
	assert.Nil(t, err)
	assert.Equal(t, illId3, illTr.ID)
}

func TestPostArchiveIllTransactionsBadRequest(t *testing.T) {
	body := httpRequest(t, "POST", "/archive_ill_transactions?archive_delay=2x&archive_status=LoanCompleted,CopyCompleted,Unfilled", nil, "", http.StatusBadRequest)
	var resp oapi.Error
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	assert.Equal(t, "time: unknown unit \"x\" in duration \"2x\"", *resp.Error)
}

func getPeers(t *testing.T) oapi.Peers {
	body := getResponseBody(t, "/peers")
	var respPeers oapi.Peers
	err := json.Unmarshal(body, &respPeers)
	assert.NoError(t, err)
	return respPeers
}

func getPeerById(t *testing.T, symbol string) oapi.Peer {
	body := getResponseBody(t, "/peers/"+symbol)
	var resp oapi.Peer
	err := json.Unmarshal(body, &resp)
	assert.NoError(t, err)
	return resp
}

func getResponseBody(t *testing.T, endpoint string) []byte {
	return httpGet(t, endpoint, "", http.StatusOK)
}

func httpRequest(t *testing.T, method string, uriPath string, reqbytes []byte, tenant string, expectStatus int) []byte {
	client := http.DefaultClient
	hreq, err := http.NewRequest(method, getLocalhostWithPort()+uriPath, bytes.NewBuffer(reqbytes))
	assert.NoError(t, err)
	if tenant != "" {
		hreq.Header.Set("X-Okapi-Tenant", tenant)
	}
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

func httpGetTrans(t *testing.T, uriPath string, tenant string, expectStatus int) []oapi.IllTransaction {
	body := httpRequest(t, "GET", uriPath, nil, tenant, expectStatus)
	var res oapi.IllTransactions
	err := json.Unmarshal(body, &res)
	assert.NoError(t, err)
	return res.Items
}

func httpGet(t *testing.T, uriPath string, tenant string, expectStatus int) []byte {
	return httpRequest(t, "GET", uriPath, nil, tenant, expectStatus)
}

func getLocalhostWithPort() string {
	return "http://localhost:" + strconv.Itoa(app.HTTP_PORT)
}
