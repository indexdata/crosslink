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
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test"
	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
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

	mockPort := strconv.Itoa(utils.Must(test.GetFreePort()))
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("HTTP_PORT", mockPort), "failed to set mock client port")
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)+"/iso18626"), "failed to set peer URL")

	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start illmock client")
	}()
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	adapter.MOCK_CLIENT_URL = "http://localhost:" + mockPort + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo = test.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

var eventRecordFormat = "%v, %v = %v"

func TestRequestLOANED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 14))
}

func TestRequestUNFILLED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, select-supplier = PROBLEM, problem=no-suppliers\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 7))

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

func TestRequestWILLSUPPLY_LOANED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 16))
}

func TestRequestWILLSUPPLY_LOANED_Cancel(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-470545f6f345"
	data, _ := os.ReadFile("../testdata/request-willsupply-loaned-cancel.xml")
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
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusCancelled) &&
			illTrans.LastRequesterAction.String == string(iso18626.TypeActionCancel)
	})
	assert.Equal(t, string(iso18626.TypeStatusCancelled), illTrans.LastSupplierStatus.String)
	assert.Equal(t, string(iso18626.TypeActionCancel), illTrans.LastRequesterAction.String)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 11))
}

func TestRequestUNFILLED_LOANED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 21))
}

func TestRequestLOANED_OVERDUE(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 16))
}

func TestRequestLOANED_OVERDUE_RENEW(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 21))
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
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
		return illTrans.LastSupplierStatus.String == "" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 8))
}

func TestRequestRETRY_COST_LOANED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
		return illTrans.LastSupplierStatus.String == "" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 21))
}

func TestRequestRETRY_ONLOAN_LOANED(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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
		return illTrans.LastSupplierStatus.String == "" &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, "", illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS\n"+
			"TASK, confirm-requester-msg = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-requester = SUCCESS\n",
		eventsToCompareString(appCtx, t, illTrans.ID, 21))
}

func eventsToCompareString(appCtx extctx.ExtendedContext, t *testing.T, illId string, messageCount int) string {
	var eventList []events.Event
	var err error

	test.WaitForPredicateToBeTrue(func() bool {
		eventList, err = eventRepo.GetIllTransactionEvents(appCtx, illId)
		if err != nil {
			t.Errorf("failed to find events for ill transaction id %v", illId)
		}
		if len(eventList) != messageCount {
			return false
		}
		for _, e := range eventList {
			if e.EventStatus == events.EventStatusProcessing || e.EventStatus == events.EventStatusNew {
				return false
			}
		}
		return true
	})

	value := ""
	for _, e := range eventList {
		value = value + fmt.Sprintf(eventRecordFormat, e.EventType, e.EventName, e.EventStatus)
		if e.EventStatus == events.EventStatusProblem {
			value += ", problem=" + e.ResultData.Problem.Kind
		}
		if e.EventStatus == events.EventStatusError {
			value += ", error=" + e.ResultData.EventError.Message
		}
		value += "\n"
	}
	return value
}

func getPgText(value string) pgtype.Text {
	return pgtype.Text{
		String: value,
		Valid:  true,
	}
}
