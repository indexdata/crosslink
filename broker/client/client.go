package client

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"net/http"
	"strings"
	"time"
)

type Iso18626Client struct {
	eventBus events.EventBus
	illRepo  ill_db.IllRepo
	client   *http.Client
}

func CreateIso18626Client(eventBus events.EventBus, illRepo ill_db.IllRepo) Iso18626Client {
	return Iso18626Client{
		eventBus: eventBus,
		illRepo:  illRepo,
		client:   http.DefaultClient,
	}
}

func CreateIso18626ClientWithHttpClient(client *http.Client) Iso18626Client {
	return Iso18626Client{
		client: client,
	}
}

func (c *Iso18626Client) MessageRequester(ctx extctx.ExtendedContext, event events.Event) {
	c.triggerNotificationsAndProcessEvent(ctx, event, c.createAndSendSupplyingAgencyMessage)
}

func (c *Iso18626Client) MessageSupplier(ctx extctx.ExtendedContext, event events.Event) {
	c.triggerNotificationsAndProcessEvent(ctx, event, c.createAndSendRequestingAgencyMessage)
}

func (c *Iso18626Client) triggerNotificationsAndProcessEvent(ctx extctx.ExtendedContext, event events.Event, h func(extctx.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) {
	err := c.eventBus.BeginTask(event.ID)
	if err != nil {
		ctx.Logger().Error("failed to start event processing", "error", err)
		return
	}

	status, result := h(ctx, event)

	err = c.eventBus.CompleteTask(event.ID, result, status)
	if err != nil {
		ctx.Logger().Error("failed to complete event processing", "error", err)
	}
}

func (c *Iso18626Client) createAndSendSupplyingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ill transaction", "error", err)
		return events.EventStatusError, nil
	}

	var resultData = map[string]any{}
	var status = events.EventStatusSuccess

	var message = &iso18626.ISO18626Message{}
	supplier, _ := c.getSupplier(ctx, illTrans)

	message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
		Header:      c.createMessageHeader(illTrans, supplier),
		MessageInfo: c.createMessageInfo(illTrans),
		StatusInfo:  c.createStatusInfo(illTrans),
	}
	resultData["message"] = message

	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		resultData["error"] = err
		ctx.Logger().Error("Failed to get requester", "error", err)
		status = events.EventStatusError
	} else {
		response, err := c.SendHttpPost(requester.Address.String, message, "")
		if response != nil {
			resultData["response"] = response
		}
		if err != nil {
			resultData["error"] = err
			ctx.Logger().Error("Failed to send ISO18626 message", "error", err)
			status = events.EventStatusError
		}
	}
	return status, &events.EventResult{
		Data: resultData,
	}
}

func (c *Iso18626Client) createAndSendRequestingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ill transaction", "error", err)
		return events.EventStatusError, nil
	}

	var resultData = map[string]any{}
	var status = events.EventStatusSuccess

	supplier, err := c.getSupplier(ctx, illTrans)
	if err != nil {
		resultData["error"] = err
		ctx.Logger().Error("Failed to get supplier", "error", err)
		status = events.EventStatusError
	} else {
		var message = &iso18626.ISO18626Message{}
		message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
			Header: c.createMessageHeader(illTrans, supplier),
			Action: iso18626.TypeActionNotification, // TODO correct action
			Note:   "",
		}
		resultData["message"] = message

		response, err := c.SendHttpPost(supplier.Address.String, message, "")
		if response != nil {
			resultData["response"] = response
		}
		if err != nil {
			resultData["error"] = err
			ctx.Logger().Error("Failed to send ISO18626 message", "error", err)
			status = events.EventStatusError
		}
	}

	return status, &events.EventResult{
		Data: resultData,
	}
}

func (c *Iso18626Client) getSupplier(ctx extctx.ExtendedContext, transaction ill_db.IllTransaction) (*ill_db.Peer, error) {
	locatedSuppliers, err := c.illRepo.GetLocatedSupplierByIllTransactionAndStatus(ctx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: transaction.ID,
		SupplierStatus: pgtype.Text{
			String: "selected",
			Valid:  true,
		},
	})
	if err != nil {
		return nil, err
	}
	if len(locatedSuppliers) == 0 {
		return nil, errors.New("missing selected supplier")
	}
	peer, err := c.illRepo.GetPeerById(ctx, locatedSuppliers[0].SupplierID)
	return &peer, err
}

func (c *Iso18626Client) createMessageHeader(transaction ill_db.IllTransaction, supplier *ill_db.Peer) iso18626.Header {
	requesterSymbol := strings.Split(transaction.RequesterSymbol.String, ":")
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := []string{"", ""}
	if supplier != nil {
		supplierSymbol = strings.Split(supplier.Symbol, ":")
	}
	return iso18626.Header{
		RequestingAgencyId: iso18626.TypeAgencyId{
			AgencyIdType:  iso18626.TypeSchemeValuePair{Text: requesterSymbol[0]},
			AgencyIdValue: requesterSymbol[1],
		},
		SupplyingAgencyId: iso18626.TypeAgencyId{
			AgencyIdType:  iso18626.TypeSchemeValuePair{Text: supplierSymbol[0]},
			AgencyIdValue: supplierSymbol[1],
		},
		Timestamp: utils.XSDDateTime{
			Time: time.Now(),
		},
		RequestingAgencyRequestId: transaction.RequesterRequestID.String,
		SupplyingAgencyRequestId:  transaction.SupplierRequestID.String,
	}
}

func (c *Iso18626Client) createMessageInfo(transaction ill_db.IllTransaction) iso18626.MessageInfo {
	reason := iso18626.TypeReasonForMessageStatusChange
	note := ""
	if transaction.RequesterAction.String == "Request" {
		reason = iso18626.TypeReasonForMessageNotification // TODO action to reason mapping
		note = "Request received"
	}
	return iso18626.MessageInfo{
		ReasonForMessage: reason,
		Note:             note,
	}
}

func (c *Iso18626Client) createStatusInfo(transaction ill_db.IllTransaction) iso18626.StatusInfo {
	status := iso18626.TypeStatusRequestReceived
	if transaction.RequesterAction.String == "Request" {
		status = iso18626.TypeStatusWillSupply // TODO action to reason mapping
	}
	return iso18626.StatusInfo{
		Status: status,
		LastChange: utils.XSDDateTime{
			Time: transaction.Timestamp.Time,
		},
	}
}

func (c *Iso18626Client) SendHttpPost(url string, msg *iso18626.ISO18626Message, tenant string) (*iso18626.ISO18626Message, error) {
	breq, _ := xml.Marshal(msg)
	if breq == nil {
		return nil, fmt.Errorf("marshal returned nil")
	}
	reader := bytes.NewReader(breq)
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml")
	if len(tenant) > 0 {
		req.Header.Set("X-Okapi-Tenant", tenant)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("%d: %s", res.StatusCode, body)
	}
	bres, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	resmsg := iso18626.ISO18626Message{}
	err = xml.Unmarshal(bres, &resmsg)
	if err != nil {
		return nil, err
	}
	return &resmsg, nil
}
