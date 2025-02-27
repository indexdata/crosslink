package client

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

var BrokerSymbol = "isil:broker"
var statusMap = map[string]iso18626.TypeStatus{
	string(iso18626.TypeStatusRequestReceived):        iso18626.TypeStatusRequestReceived,
	string(iso18626.TypeStatusExpectToSupply):         iso18626.TypeStatusExpectToSupply,
	string(iso18626.TypeStatusWillSupply):             iso18626.TypeStatusWillSupply,
	string(iso18626.TypeStatusLoaned):                 iso18626.TypeStatusLoaned,
	string(iso18626.TypeStatusOverdue):                iso18626.TypeStatusOverdue,
	string(iso18626.TypeStatusRecalled):               iso18626.TypeStatusRecalled,
	string(iso18626.TypeStatusRetryPossible):          iso18626.TypeStatusRetryPossible,
	string(iso18626.TypeStatusUnfilled):               iso18626.TypeStatusUnfilled,
	string(iso18626.TypeStatusCopyCompleted):          iso18626.TypeStatusCopyCompleted,
	string(iso18626.TypeStatusLoanCompleted):          iso18626.TypeStatusLoanCompleted,
	string(iso18626.TypeStatusCompletedWithoutReturn): iso18626.TypeStatusCompletedWithoutReturn,
	string(iso18626.TypeStatusCancelled):              iso18626.TypeStatusCancelled,
}

var RequestAction = "Request"
var actionMap = map[string]iso18626.TypeAction{
	string(iso18626.TypeActionStatusRequest):  iso18626.TypeActionStatusRequest,
	string(iso18626.TypeActionReceived):       iso18626.TypeActionReceived,
	string(iso18626.TypeActionCancel):         iso18626.TypeActionCancel,
	string(iso18626.TypeActionRenew):          iso18626.TypeActionRenew,
	string(iso18626.TypeActionShippedReturn):  iso18626.TypeActionShippedReturn,
	string(iso18626.TypeActionShippedForward): iso18626.TypeActionShippedForward,
	string(iso18626.TypeActionNotification):   iso18626.TypeActionNotification,
}

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
	c.triggerNotificationsAndProcessEvent(ctx, event, c.createAndSendRequestOrRequestingAgencyMessage)
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
	locSupplier, peer, _ := c.getSupplier(ctx, illTrans)
	statusInfo, statusErr := c.createStatusInfo(illTrans, locSupplier)

	message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
		Header:      c.createMessageHeader(illTrans, peer, false),
		MessageInfo: c.createMessageInfo(),
		StatusInfo:  statusInfo,
	}
	resultData["message"] = message

	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		resultData["error"] = err.Error()
		ctx.Logger().Error("Failed to get requester", "error", err)
		status = events.EventStatusError
	} else if statusErr != nil {
		resultData["error"] = statusErr.Error()
		ctx.Logger().Error("failed to get status", "error", statusErr)
		status = events.EventStatusError
	} else {
		response, err := c.SendHttpPost(requester.Url, message, "")
		if response != nil {
			resultData["response"] = response
		}
		if err != nil {
			resultData["error"] = err.Error()
			ctx.Logger().Error("Failed to send ISO18626 message", "error", err)
			status = events.EventStatusError
		}
	}
	return status, &events.EventResult{
		Data: resultData,
	}
}

func (c *Iso18626Client) createAndSendRequestOrRequestingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ill transaction", "error", err)
		return events.EventStatusError, nil
	}

	var resultData = map[string]any{}
	var status = events.EventStatusSuccess

	selected, peer, err := c.getSupplier(ctx, illTrans)
	if err != nil {
		resultData["error"] = err
		ctx.Logger().Error("Failed to get supplier", "error", err)
		status = events.EventStatusError
	} else {
		var message = &iso18626.ISO18626Message{}
		internalErr := ""
		if illTrans.LastRequesterAction.String == RequestAction {
			message.Request = &iso18626.Request{
				Header:                c.createMessageHeader(illTrans, peer, true),
				BibliographicInfo:     illTrans.IllTransactionData.BibliographicInfo,
				PublicationInfo:       illTrans.IllTransactionData.PublicationInfo,
				ServiceInfo:           illTrans.IllTransactionData.ServiceInfo,
				SupplierInfo:          illTrans.IllTransactionData.SupplierInfo,
				RequestedDeliveryInfo: illTrans.IllTransactionData.RequestedDeliveryInfo,
				PatronInfo:            illTrans.IllTransactionData.PatronInfo,
				BillingInfo:           illTrans.IllTransactionData.BillingInfo,
				RequestingAgencyInfo:  illTrans.IllTransactionData.RequestingAgencyInfo,
			}
			message.Request.BibliographicInfo.SupplierUniqueRecordId = selected.LocalID.String
			c.updateSelectedSupplierAction(&selected, RequestAction)
		} else {
			var action iso18626.TypeAction
			found, ok := actionMap[illTrans.LastRequesterAction.String]
			if ok {
				action = found
			} else {
				internalErr = "did not find action for value: " + illTrans.LastRequesterAction.String
			}
			message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
				Header: c.createMessageHeader(illTrans, peer, true),
				Action: action,
				Note:   "",
			}
			c.updateSelectedSupplierAction(&selected, string(action))
		}
		resultData["message"] = message
		if internalErr != "" {
			resultData["error"] = internalErr
			ctx.Logger().Error("failed to create message", "error", internalErr)
			status = events.EventStatusProblem
		} else {
			response, err := c.SendHttpPost(peer.Url, message, "")
			if response != nil {
				resultData["response"] = response
			}
			if err != nil {
				resultData["error"] = err
				ctx.Logger().Error("Failed to send ISO18626 message", "error", err)
				status = events.EventStatusError
			}
			utils.Must(c.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(selected)))
		}
	}

	return status, &events.EventResult{
		Data: resultData,
	}
}

func (c *Iso18626Client) updateSelectedSupplierAction(sup *ill_db.LocatedSupplier, action string) {
	sup.PrevAction = sup.LastAction
	sup.LastAction = pgtype.Text{
		String: action,
		Valid:  true,
	}
}

func (c *Iso18626Client) getSupplier(ctx extctx.ExtendedContext, transaction ill_db.IllTransaction) (ill_db.LocatedSupplier, *ill_db.Peer, error) {
	locatedSuppliers, err := c.illRepo.GetLocatedSupplierByIllTransactionAndStatus(ctx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: transaction.ID,
		SupplierStatus: pgtype.Text{
			String: "selected",
			Valid:  true,
		},
	})
	if err != nil {
		return ill_db.LocatedSupplier{}, nil, err
	}
	if len(locatedSuppliers) == 0 {
		return ill_db.LocatedSupplier{}, nil, errors.New("missing selected supplier")
	}
	peer, err := c.illRepo.GetPeerById(ctx, locatedSuppliers[0].SupplierID)
	return locatedSuppliers[0], &peer, err
}

func (c *Iso18626Client) createMessageHeader(transaction ill_db.IllTransaction, supplier *ill_db.Peer, hideRequester bool) iso18626.Header {
	requesterSymbol := strings.Split(transaction.RequesterSymbol.String, ":")
	if hideRequester {
		requesterSymbol = strings.Split(BrokerSymbol, ":")
	}
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := strings.Split(BrokerSymbol, ":")
	if supplier != nil && hideRequester {
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

func (c *Iso18626Client) createMessageInfo() iso18626.MessageInfo {
	return iso18626.MessageInfo{
		ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
	}
}

func (c *Iso18626Client) createStatusInfo(transaction ill_db.IllTransaction, supplier ill_db.LocatedSupplier) (iso18626.StatusInfo, error) {
	var status iso18626.TypeStatus
	if s, ok := statusMap[supplier.LastStatus.String]; ok {
		status = s
	} else {
		return iso18626.StatusInfo{}, errors.New("failed to resolve status for value: " + supplier.LastStatus.String)
	}
	return iso18626.StatusInfo{
		Status: status,
		LastChange: utils.XSDDateTime{
			Time: transaction.Timestamp.Time,
		},
	}, nil
}

func (c *Iso18626Client) SendHttpPost(url string, msg *iso18626.ISO18626Message, tenant string) (*iso18626.ISO18626Message, error) {
	breq := utils.Must(xml.Marshal(msg))
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
