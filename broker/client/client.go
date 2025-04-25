package client

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/shim"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

var BrokerSymbol = "ISIL:BROKER"

type BrokerMode string

const (
	BrokerModeOpaque      BrokerMode = "opaque"
	BrokerModeTransparent BrokerMode = "transparent"
)

type Iso18626Client struct {
	eventBus   events.EventBus
	illRepo    ill_db.IllRepo
	client     *http.Client
	maxMsgSize int
	brokerMode BrokerMode
}

func CreateIso18626Client(eventBus events.EventBus, illRepo ill_db.IllRepo, maxMsgSize int, brokerMode BrokerMode) Iso18626Client {
	return Iso18626Client{
		eventBus:   eventBus,
		illRepo:    illRepo,
		client:     http.DefaultClient,
		maxMsgSize: maxMsgSize,
		brokerMode: brokerMode,
	}
}

func CreateIso18626ClientWithHttpClient(client *http.Client) Iso18626Client {
	return Iso18626Client{
		client: client,
	}
}

func (c *Iso18626Client) MessageRequester(ctx extctx.ExtendedContext, event events.Event) {
	c.eventBus.ProcessTask(ctx, event, c.createAndSendSupplyingAgencyMessage)
}

func (c *Iso18626Client) MessageSupplier(ctx extctx.ExtendedContext, event events.Event) {
	c.eventBus.ProcessTask(ctx, event, c.createAndSendRequestOrRequestingAgencyMessage)
}

func (c *Iso18626Client) createAndSendSupplyingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ILL transaction", "error", err)
		return events.EventStatusError, nil
	}
	resData := events.EventResult{}

	locSupplier, _, _ := c.getSupplier(ctx, illTrans)
	var status iso18626.TypeStatus
	if locSupplier == nil {
		status = iso18626.TypeStatusUnfilled
	} else {
		if s, ok := iso18626.StatusMap[locSupplier.LastStatus.String]; ok {
			status = s
		} else {
			msg := "failed to resolve status for value: " + locSupplier.LastStatus.String
			resData.EventError = &events.EventError{
				Message: msg,
			}
			ctx.Logger().Error(msg)
			return events.EventStatusError, &resData
		}
		//in opaque mode we proxy ExpectToSupply and WillSupply once
		if c.brokerMode == BrokerModeOpaque {
			lastSentStatus := illTrans.LastSupplierStatus.String
			if len(lastSentStatus) > 0 {
				if status == iso18626.TypeStatusExpectToSupply && lastSentStatus != string(iso18626.TypeStatusRequestReceived) {
					resData.Note = "status ExpectToSupply may have already been communicated and will be ignored"
					return events.EventStatusSuccess, &resData
				}
				if status == iso18626.TypeStatusWillSupply && lastSentStatus != string(iso18626.TypeStatusRequestReceived) &&
					lastSentStatus != string(iso18626.TypeStatusExpectToSupply) {
					resData.Note = "status WillSupply may have already been communicated and will be ignored"
					return events.EventStatusSuccess, &resData
				}
			}
		}
	}
	var message *iso18626.ISO18626Message
	if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.SupplyingAgencyMessage != nil {
		message = event.EventData.IncomingMessage
	} else {
		message = &iso18626.ISO18626Message{}
		message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	}

	message.SupplyingAgencyMessage.Header = c.createMessageHeader(illTrans, locSupplier, false)
	message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = c.getReasonFromAction(illTrans.LastRequesterAction.String)
	message.SupplyingAgencyMessage.StatusInfo.Status = status
	message.SupplyingAgencyMessage.StatusInfo.LastChange = utils.XSDDateTime{Time: time.Now()}

	resData.OutgoingMessage = message

	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to get requester",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to get requester", "error", err)
		return events.EventStatusError, &resData
	}
	response, err := c.SendHttpPost(&requester, message, "")
	if response != nil {
		resData.IncomingMessage = response
	}
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to send ISO18626 message",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to send ISO18626 message", "error", err)
		return events.EventStatusError, &resData
	}
	err = c.updateSupplierStatus(ctx, event.IllTransactionID, string(message.SupplyingAgencyMessage.StatusInfo.Status))
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to update supplier status",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to update supplier status", "error", err)
		return events.EventStatusError, &resData
	}
	return events.EventStatusSuccess, &resData
}

func (c *Iso18626Client) updateSupplierStatus(ctx extctx.ExtendedContext, id string, status string) error {
	err := c.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		illTrans, err := repo.GetIllTransactionByIdForUpdate(ctx, id)
		if err != nil {
			return err
		}
		illTrans.PrevSupplierStatus = illTrans.LastSupplierStatus
		illTrans.LastSupplierStatus = pgtype.Text{
			String: status,
			Valid:  true,
		}
		_, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
		return err
	})
	return err
}

func (c *Iso18626Client) createAndSendRequestOrRequestingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ILL transaction", "error", err)
		return events.EventStatusError, nil
	}

	resData := events.EventResult{}
	selected, peer, err := c.getSupplier(ctx, illTrans)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to get supplier",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to get supplier", "error", err)
		return events.EventStatusError, &resData
	}
	var isRequest = illTrans.LastRequesterAction.String == ill_db.RequestAction
	var status = events.EventStatusSuccess
	var message = &iso18626.ISO18626Message{}
	var action string
	if isRequest {
		message.Request = &iso18626.Request{
			Header:                c.createMessageHeader(illTrans, selected, true),
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
		action = ill_db.RequestAction
	} else {
		found, ok := iso18626.ActionMap[illTrans.LastRequesterAction.String]
		if !ok {
			var internalErr = "did not find action for value: " + illTrans.LastRequesterAction.String
			resData.EventError = &events.EventError{
				Message: "failed to create message",
				Cause:   internalErr,
			}
			ctx.Logger().Error("failed to create message", "error", internalErr)
			return events.EventStatusError, &resData
		}
		var note = ""
		if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.RequestingAgencyMessage != nil {
			note = event.EventData.IncomingMessage.RequestingAgencyMessage.Note
		}
		message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
			Header: c.createMessageHeader(illTrans, selected, true),
			Action: found,
			Note:   note,
		}
		action = string(found)
	}
	resData.OutgoingMessage = message
	response, err := c.SendHttpPost(peer, message, "")
	if response != nil {
		resData.IncomingMessage = response
	}
	if err != nil {
		var httpErr *httpclient.HttpError
		if errors.As(err, &httpErr) {
			resData.HttpFailure = httpErr
		}
		resData.EventError = &events.EventError{
			Message: "failed to send ISO18626 message",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to send ISO18626 message", "error", err)
		status = events.EventStatusError
	} else {
		status = c.checkConfirmationError(isRequest, response, status)
	}
	// check for status == EvenStatusError and NOT save??
	err = c.updateSelectedSupplierAction(ctx, illTrans.ID, action)
	if err != nil {
		ctx.Logger().Error("failed updating supplier", "error", err)
		resData.EventError = &events.EventError{
			Message: "failed updating supplier",
			Cause:   err.Error(),
		}
		status = events.EventStatusError
	}
	return status, &resData
}

func (c *Iso18626Client) updateSelectedSupplierAction(ctx extctx.ExtendedContext, id string, action string) error {
	return c.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		locsup, err := repo.GetSelectedSupplierForIllTransactionForUpdate(ctx, id)
		if err != nil {
			return err // transaction gone meanwhile
		}
		locsup.PrevAction = locsup.LastAction
		locsup.LastAction = pgtype.Text{
			String: action,
			Valid:  true,
		}
		_, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locsup))
		return err
	})
}

func (c *Iso18626Client) getSupplier(ctx extctx.ExtendedContext, transaction ill_db.IllTransaction) (*ill_db.LocatedSupplier, *ill_db.Peer, error) {
	selectedSupplier, err := c.illRepo.GetSelectedSupplierForIllTransaction(ctx, transaction.ID)
	if err != nil {
		return nil, nil, err
	}
	peer, err := c.illRepo.GetPeerById(ctx, selectedSupplier.SupplierID)
	if err != nil {
		return nil, nil, err
	}
	return &selectedSupplier, &peer, err
}

func (c *Iso18626Client) createMessageHeader(transaction ill_db.IllTransaction, sup *ill_db.LocatedSupplier, isRequestingMessage bool) iso18626.Header {
	requesterSymbol := strings.Split(BrokerSymbol, ":")
	if !isRequestingMessage || c.brokerMode == BrokerModeTransparent {
		requesterSymbol = strings.Split(transaction.RequesterSymbol.String, ":")
	}
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := strings.Split(BrokerSymbol, ":")
	if sup != nil && sup.SupplierSymbol != "" && (isRequestingMessage || c.brokerMode == BrokerModeTransparent) {
		supplierSymbol = strings.Split(sup.SupplierSymbol, ":")
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

func (c *Iso18626Client) getReasonFromAction(requesterAction string) iso18626.TypeReasonForMessage {
	var reason iso18626.TypeReasonForMessage
	switch requesterAction {
	case ill_db.RequestAction:
		reason = iso18626.TypeReasonForMessageRequestResponse
	case string(iso18626.TypeActionStatusRequest):
		reason = iso18626.TypeReasonForMessageStatusRequestResponse
	case string(iso18626.TypeActionNotification):
		reason = iso18626.TypeReasonForMessageNotification
	case string(iso18626.TypeActionRenew):
		reason = iso18626.TypeReasonForMessageRenewResponse
	case string(iso18626.TypeActionCancel):
		reason = iso18626.TypeReasonForMessageCancelResponse
	default:
		reason = iso18626.TypeReasonForMessageStatusChange
	}
	return reason
}

func (c *Iso18626Client) checkConfirmationError(isRequest bool, response *iso18626.ISO18626Message, defaultStatus events.EventStatus) events.EventStatus {
	if isRequest && response.RequestConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		return events.EventStatusProblem
	} else if !isRequest && response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		return events.EventStatusProblem
	}
	return defaultStatus
}

func (c *Iso18626Client) SendHttpPost(peer *ill_db.Peer, msg *iso18626.ISO18626Message, tenant string) (*iso18626.ISO18626Message, error) {
	httpClient := httpclient.NewClient().WithMaxSize(int64(c.maxMsgSize))
	if len(tenant) > 0 {
		httpClient.WithHeaders("X-Okapi-Tenant", tenant)
	}
	iso18626Shim := shim.GetShim(peer.Vendor)
	var resmsg iso18626.ISO18626Message
	err := httpClient.RequestResponse(c.client, http.MethodPost,
		[]string{httpclient.ContentTypeApplicationXml, httpclient.ContentTypeTextXml},
		peer.Url, msg, &resmsg, func(v any) ([]byte, error) {
			return iso18626Shim.ApplyToOutgoing(v.(*iso18626.ISO18626Message))
		}, func(b []byte, v any) error {
			return iso18626Shim.ApplyToIncoming(b, v.(*iso18626.ISO18626Message))
		})
	if err != nil {
		return nil, err
	}
	return &resmsg, nil
}
