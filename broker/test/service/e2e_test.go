package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/events"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/dbutil"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
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

func TestMain(m *testing.M) {
	ill_db.PeerRefreshInterval = 0 //force refresh for every test
	ctx := context.Background()
	dbutil.DB_PROVISION = true
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

	mockPort := utils.Must(test.GetFreePort())
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)+"/iso18626"), "failed to set peer URL")

	apptest.StartMockApp(mockPort)
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	adapter.MOCK_CLIENT_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestRequestLOANED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f343"
	data, _ := os.ReadFile("../testdata/request-loaned.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
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

func TestRequestUNFILLED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f342"
	data, _ := os.ReadFile("../testdata/request-unfilled.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusUnfilled) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, string(iso18626.TypeStatusUnfilled), illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = PROBLEM, problem=no-suppliers\n" +
		"TASK, message-requester = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)

	data, err = os.ReadFile("../testdata/request-retry-after-unfilled.xml")
	assert.Nil(t, err)
	brokerUrl := os.Getenv("PEER_URL")
	req, err = http.NewRequest("POST", brokerUrl, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client = &http.Client{}
	res, err = client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	assert.Nil(t, err)
	var msg iso18626.Iso18626MessageNS
	err = xml.Unmarshal(body, &msg)
	assert.Nil(t, err)
	assert.NotNil(t, msg.RequestConfirmation)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, msg.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, msg.RequestConfirmation.ErrorData.ErrorType)
	assert.Equal(t, string(handler.RetryNotPossible), msg.RequestConfirmation.ErrorData.ErrorValue)
}

func TestMessageAfterUNFILLED(t *testing.T) {
	adapter.DEFAULT_BROKER_MODE = common.BrokerModeTransparent
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f352"
	data, _ := os.ReadFile("../testdata/request-unfilled-2.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusUnfilled) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, string(iso18626.TypeStatusUnfilled), illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = PROBLEM, problem=no-suppliers\n" +
		"TASK, message-requester = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
	data, err = os.ReadFile("../testdata/supmsg-notification.xml")
	assert.Nil(t, err)
	brokerUrl := os.Getenv("PEER_URL")
	req, err = http.NewRequest("POST", brokerUrl, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client = &http.Client{}
	res, err = client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	assert.Nil(t, err)
	var msg iso18626.Iso18626MessageNS
	err = xml.Unmarshal(body, &msg)
	assert.Nil(t, err)
	assert.NotNil(t, msg.SupplyingAgencyMessageConfirmation)
	// Notification was forwarded -> status ERRPR
	assert.Equal(t, iso18626.TypeMessageStatusERROR, msg.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	//wait until the broker processes the notification
	//this relies on the fact that the broker will update the previous transaction status AFTER sending out the notification
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.PrevSupplierStatus.String == string(iso18626.TypeStatusUnfilled) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	adapter.DEFAULT_BROKER_MODE = common.BrokerModeOpaque
}

func TestMessageSkipped(t *testing.T) {
	adapter.DEFAULT_BROKER_MODE = common.BrokerModeTransparent
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f362"
	data, _ := os.ReadFile("../testdata/request-unfilled-willsupply.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusWillSupply) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, string(iso18626.TypeStatusWillSupply), illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
	data, err = os.ReadFile("../testdata/supmsg-notification-2.xml")
	assert.Nil(t, err)
	brokerUrl := os.Getenv("PEER_URL")
	req, err = http.NewRequest("POST", brokerUrl, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client = &http.Client{}
	res, err = client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	assert.Nil(t, err)
	var msg iso18626.Iso18626MessageNS
	err = xml.Unmarshal(body, &msg)
	assert.Nil(t, err)
	assert.NotNil(t, msg.SupplyingAgencyMessageConfirmation)
	assert.Equal(t, iso18626.TypeMessageStatusOK, msg.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	//wait until the broker processes the notification
	//this relies on the fact that the broker will update the previous transaction status AFTER sending out the notification
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.PrevSupplierStatus.String == string(iso18626.TypeStatusWillSupply) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	adapter.DEFAULT_BROKER_MODE = common.BrokerModeOpaque
}

func TestRequestWILLSUPPLY_LOANED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f344"
	data, _ := os.ReadFile("../testdata/request-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
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

func TestRequestWILLSUPPLY_LOANED_Cancel_BrokerModeOpaque_Broker(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requester := apptest.CreatePeerWithMode(t, illRepo, "ISIL:REQ-CANCEL-0", adapter.MOCK_CLIENT_URL, string(common.BrokerModeOpaque))
	reqId := "5636c993-c41c-48f4-a285-470545f6f345-0"
	data, _ := os.ReadFile("../testdata/request-willsupply-loaned-cancel.xml")
	stringData := strings.ReplaceAll(string(data), "{index}", "0")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader([]byte(stringData)))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusCancelled) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionCancel)
	})
	assert.Equal(t, string(iso18626.TypeStatusCancelled), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionCancel), illTrans.LastRequesterAction.String)
	assert.Equal(t, requester.ID, illTrans.RequesterID.String)
	assert.Equal(t, "NOTICE, request-received = SUCCESS\n"+
		"TASK, locate-suppliers = SUCCESS\n"+
		"TASK, select-supplier = SUCCESS\n"+
		"TASK, message-requester = SUCCESS, reason=Notification, ExpectToSupply\n"+
		"TASK, message-supplier = SUCCESS, Request\n"+
		"NOTICE, supplier-msg-received = SUCCESS, reason=RequestResponse, WillSupply\n"+
		"TASK, message-requester = SUCCESS, reason=StatusChange, WillSupply\n"+
		"TASK, confirm-supplier-msg = SUCCESS\n"+
		"NOTICE, requester-msg-received = SUCCESS, Cancel\n"+
		"TASK, message-supplier = SUCCESS, Cancel\n"+
		"TASK, confirm-requester-msg = SUCCESS\n"+
		"NOTICE, supplier-msg-received = SUCCESS, reason=CancelResponse, Cancelled\n"+
		"TASK, message-requester = SUCCESS, reason=CancelResponse, Cancelled\n"+
		"TASK, confirm-supplier-msg = SUCCESS\n",
		apptest.EventsToCompareStringFunc(appCtx, eventRepo, t, illTrans.ID, 14, false, formatEvent))
}

func TestRequestWILLSUPPLY_LOANED_Cancel_BrokerModeTransparent_Supplier(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requester := apptest.CreatePeerWithMode(t, illRepo, "ISIL:REQ-CANCEL-3", adapter.MOCK_CLIENT_URL, string(common.BrokerModeTransparent))
	reqId := "5636c993-c41c-48f4-a285-470545f6f345-3"
	data, _ := os.ReadFile("../testdata/request-willsupply-loaned-cancel.xml")
	stringData := strings.ReplaceAll(strings.ReplaceAll(string(data), "{index}", "3"), "BROKER", "SUP1")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader([]byte(stringData)))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	assert.Equal(t, requester.ID, illTrans.RequesterID.String)
	assert.Equal(t, "NOTICE, request-received = SUCCESS\n"+
		"TASK, locate-suppliers = SUCCESS\n"+
		"TASK, select-supplier = SUCCESS\n"+
		"TASK, message-requester = SUCCESS, reason=RequestResponse, ExpectToSupply\n"+
		"TASK, message-supplier = SUCCESS, Request\n"+
		"NOTICE, requester-msg-received = SUCCESS, Cancel\n"+
		"TASK, message-supplier = SUCCESS, Cancel\n"+
		"TASK, confirm-requester-msg = SUCCESS\n"+
		"NOTICE, supplier-msg-received = SUCCESS, reason=CancelResponse, Cancelled\n"+
		"TASK, confirm-supplier-msg = SUCCESS\n"+
		"TASK, select-supplier = SUCCESS\n"+
		"TASK, message-requester = SUCCESS, reason=StatusChange, ExpectToSupply\n"+
		"TASK, message-supplier = SUCCESS, Request\n"+
		"NOTICE, supplier-msg-received = SUCCESS, reason=RequestResponse, Loaned\n"+
		"TASK, message-requester = SUCCESS, reason=StatusChange, Loaned\n"+
		"TASK, confirm-supplier-msg = SUCCESS\n"+
		"NOTICE, requester-msg-received = SUCCESS, Received\n"+
		"TASK, message-supplier = SUCCESS, Received\n"+
		"TASK, confirm-requester-msg = SUCCESS\n"+
		"NOTICE, requester-msg-received = SUCCESS, ShippedReturn\n"+
		"TASK, message-supplier = SUCCESS, ShippedReturn\n"+
		"TASK, confirm-requester-msg = SUCCESS\n"+
		"NOTICE, supplier-msg-received = SUCCESS, reason=StatusChange, LoanCompleted\n"+
		"TASK, message-requester = SUCCESS, reason=StatusChange, LoanCompleted\n"+
		"TASK, confirm-supplier-msg = SUCCESS\n",
		apptest.EventsToCompareStringFunc(appCtx, eventRepo, t, illTrans.ID, 25, false, formatEvent))
}

func TestRequestUNFILLED_LOANED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f341"
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
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

func TestRequestLOANED_OVERDUE(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "20e99395-4c3b-4229-ab91-49d5f7073188"
	data, _ := os.ReadFile("../testdata/request-loaned-overdue.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

func TestRequestLOANED_OVERDUE_RENEW(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "20e99395-4c3b-4229-ab91-49d5f7073189"
	data, _ := os.ReadFile("../testdata/request-loaned-overdue-renew.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusLoanCompleted) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionShippedReturn)
	})
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionShippedReturn), illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"TASK, confirm-requester-msg = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

func TestRequestRETRY_NON_EXISTING(t *testing.T) {
	data, err := os.ReadFile("../testdata/request-retry-non-existing.xml")
	assert.Nil(t, err)
	brokerUrl := os.Getenv("PEER_URL")
	req, _ := http.NewRequest("POST", brokerUrl, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	assert.Nil(t, err)
	var msg iso18626.Iso18626MessageNS
	err = xml.Unmarshal(body, &msg)
	assert.Nil(t, err)
	assert.NotNil(t, msg.RequestConfirmation)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, msg.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, msg.RequestConfirmation.ErrorData.ErrorType)
	assert.Equal(t, string(handler.RetryNotPossible), msg.RequestConfirmation.ErrorData.ErrorValue)
}

func TestRequestREMINDER(t *testing.T) {
	data, err := os.ReadFile("../testdata/request-reminder.xml")
	assert.Nil(t, err)
	brokerUrl := os.Getenv("PEER_URL")
	req, _ := http.NewRequest("POST", brokerUrl, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	assert.Nil(t, err)
	var msg iso18626.Iso18626MessageNS
	err = xml.Unmarshal(body, &msg)
	assert.Nil(t, err)
	assert.NotNil(t, msg.RequestConfirmation)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, msg.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, msg.RequestConfirmation.ErrorData.ErrorType)
	assert.Equal(t, string(handler.UnsupportedRequestType), msg.RequestConfirmation.ErrorData.ErrorValue)
}

func TestRequestRETRY_COST(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "fc60b4fa-5f98-49a8-a2a0-f17b76fa16a8"
	data, err := os.ReadFile("../testdata/request-retry-1.xml")
	assert.Nil(t, err)
	req, err := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "RetryPossible" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "RetryPossible", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n"
	apptest.EventsCompareString(appCtx, eventRepo, t, illTrans.ID, exp)
}

func TestRequestRETRY_COST_LOANED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f346"
	data, err := os.ReadFile("../testdata/request-retry-cost-loaned.xml")
	assert.Nil(t, err)
	req, err := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
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
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
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

func TestRequestRETRY_ONLOAN_LOANED(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "f8ef7750-982d-41bc-a123-0e18169a0018"
	data, err := os.ReadFile("../testdata/request-retry-onloan-loaned.xml")
	assert.Nil(t, err)
	req, err := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	assert.Nil(t, err)
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, getPgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == "LoanCompleted" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "LoanCompleted", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "ShippedReturn", illTrans.LastRequesterAction.String)
	exp := "NOTICE, request-received = SUCCESS\n" +
		"TASK, locate-suppliers = SUCCESS\n" +
		"TASK, select-supplier = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, message-supplier = SUCCESS\n" +
		"NOTICE, supplier-msg-received = SUCCESS\n" +
		"TASK, message-requester = SUCCESS\n" +
		"TASK, confirm-supplier-msg = SUCCESS\n" +
		"NOTICE, requester-msg-received = SUCCESS\n" +
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

func getPgText(value string) pgtype.Text {
	return pgtype.Text{
		String: value,
		Valid:  true,
	}
}

func formatEvent(e events.Event) string {
	if e.EventName == "message-supplier" {
		if e.ResultData.OutgoingMessage.RequestingAgencyMessage != nil {
			return fmt.Sprintf(apptest.EventRecordFormat+", %v", e.EventType, e.EventName, e.EventStatus, e.ResultData.OutgoingMessage.RequestingAgencyMessage.Action)
		} else {
			return fmt.Sprintf(apptest.EventRecordFormat+", %v", e.EventType, e.EventName, e.EventStatus, "Request")
		}
	}
	if e.EventName == "message-requester" {
		return fmt.Sprintf(apptest.EventRecordFormat+", reason=%v, %v", e.EventType, e.EventName, e.EventStatus, e.ResultData.OutgoingMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage, e.ResultData.OutgoingMessage.SupplyingAgencyMessage.StatusInfo.Status)
	}
	if e.EventName == "supplier-msg-received" {
		return fmt.Sprintf(apptest.EventRecordFormat+", reason=%v, %v", e.EventType, e.EventName, e.EventStatus, e.EventData.IncomingMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage, e.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo.Status)
	}
	if e.EventName == "requester-msg-received" {
		return fmt.Sprintf(apptest.EventRecordFormat+", %v", e.EventType, e.EventName, e.EventStatus, e.EventData.IncomingMessage.RequestingAgencyMessage.Action)
	}
	return fmt.Sprintf(apptest.EventRecordFormat, e.EventType, e.EventName, e.EventStatus)
}
