package client

import (
	"errors"
	"fmt"
	"github.com/indexdata/crosslink/broker/adapter"
	"net/http"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/shim"
	"github.com/indexdata/crosslink/broker/vcs"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

var BrokerSymbol = "ISIL:BROKER"

type Iso18626Client struct {
	eventBus   events.EventBus
	illRepo    ill_db.IllRepo
	client     *http.Client
	maxMsgSize int
	sendDelay  time.Duration
}

func CreateIso18626Client(eventBus events.EventBus, illRepo ill_db.IllRepo, maxMsgSize int, delay time.Duration) Iso18626Client {
	return Iso18626Client{
		eventBus:   eventBus,
		illRepo:    illRepo,
		client:     http.DefaultClient,
		maxMsgSize: maxMsgSize,
		sendDelay:  delay,
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
	resData := events.EventResult{}
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to read ILL transaction",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to read ILL transaction", "error", err)
		return events.EventStatusError, nil
	}

	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to get requester",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to get requester", "error", err)
		return events.EventStatusError, &resData
	}

	locSupplier, supplier, _ := c.getSupplier(ctx, illTrans)
	var status iso18626.TypeStatus
	if locSupplier == nil {
		status = iso18626.TypeStatusUnfilled
	} else if illTrans.RequesterID.Valid && locSupplier.SupplierID == illTrans.RequesterID.String && !locSupplier.LastStatus.Valid {
		status = iso18626.TypeStatusExpectToSupply
	} else {
		if s, ok := iso18626.StatusMap[locSupplier.LastStatus.String]; ok {
			status = s
		} else if !locSupplier.LastStatus.Valid {
			if requester.BrokerMode == string(extctx.BrokerModeTransparent) {
				status = iso18626.TypeStatusExpectToSupply
			} else {
				resData.Note = "no need to message requester in broker mode " + requester.BrokerMode
				return events.EventStatusSuccess, &resData
			}
		} else {
			msg := "failed to resolve status for value: " + locSupplier.LastStatus.String
			resData.EventError = &events.EventError{
				Message: msg,
			}
			ctx.Logger().Error(msg)
			return events.EventStatusError, &resData
		}
		//in opaque mode we proxy ExpectToSupply and WillSupply once
		if requester.BrokerMode == string(extctx.BrokerModeOpaque) {
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

	brokerMode := string(adapter.DEFAULT_BROKER_MODE)
	if supplier != nil {
		brokerMode = supplier.BrokerMode
	}

	message.SupplyingAgencyMessage.Header = c.createMessageHeader(illTrans, locSupplier, false, brokerMode)
	reason := message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage
	message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = c.validateReason(reason, illTrans.LastRequesterAction.String, illTrans.LastSupplierStatus.String)
	message.SupplyingAgencyMessage.StatusInfo.Status = status
	message.SupplyingAgencyMessage.StatusInfo.LastChange = utils.XSDDateTime{Time: time.Now()}

	if status == iso18626.TypeStatusLoaned {
		name, agencyId, address := getPeerNameAgencyIdAddress(supplier, locSupplier.SupplierSymbol)
		populateReturnAddress(message, name, agencyId, address)
	}

	resData.OutgoingMessage = message
	if !isDoNotSend(event) {
		response, err := c.SendHttpPost(&requester, message)
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
	} else {
		if resData.CustomData == nil {
			resData.CustomData = map[string]any{}
		}
		resData.CustomData["doNotSend"] = true
		resData.OutgoingMessage = nil
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

func populateReturnAddress(message *iso18626.ISO18626Message, name string, agencyId iso18626.TypeAgencyId, address iso18626.PhysicalAddress) {
	if message.SupplyingAgencyMessage.ReturnInfo == nil {
		message.SupplyingAgencyMessage.ReturnInfo = &iso18626.ReturnInfo{}
	}
	if message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId == nil {
		message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId = &agencyId
	}
	if message.SupplyingAgencyMessage.ReturnInfo.Name == "" {
		message.SupplyingAgencyMessage.ReturnInfo.Name = name
	}
	if message.SupplyingAgencyMessage.ReturnInfo.PhysicalAddress == nil {
		message.SupplyingAgencyMessage.ReturnInfo.PhysicalAddress = &address
	}
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
	resData := events.EventResult{}
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to read ILL transaction",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to read ILL transaction", "error", err)
		return events.EventStatusError, &resData
	}
	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		resData.EventError = &events.EventError{
			Message: "failed to get requester",
			Cause:   err.Error(),
		}
		ctx.Logger().Error("failed to get requester", "error", err)
		return events.EventStatusError, &resData
	}

	selected, supplier, err := c.getSupplier(ctx, illTrans)
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
			Header:                c.createMessageHeader(illTrans, selected, true, supplier.BrokerMode),
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
		requesterName, _, deliveryAddress := getPeerNameAgencyIdAddress(&requester, illTrans.RequesterSymbol.String)
		populateRequesterInfo(message, requesterName, deliveryAddress)
		populateDeliveryAddress(message, requesterName, deliveryAddress)
		supplierName, suppAgencyId, supplierAddress := getPeerNameAgencyIdAddress(supplier, selected.SupplierSymbol)
		populateSupplierInfo(message, supplierName, suppAgencyId, supplierAddress)
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
			Header: c.createMessageHeader(illTrans, selected, true, supplier.BrokerMode),
			Action: found,
			Note:   note,
		}
		action = string(found)
	}
	resData.OutgoingMessage = message
	if !isDoNotSend(event) {
		response, err := c.SendHttpPost(supplier, message)
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
	} else {
		if resData.CustomData == nil {
			resData.CustomData = map[string]any{}
		}
		resData.CustomData["doNotSend"] = true
		resData.OutgoingMessage = nil
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

func populateRequesterInfo(message *iso18626.ISO18626Message, name string, address iso18626.PhysicalAddress) {
	if message.Request.RequestingAgencyInfo == nil || message.Request.RequestingAgencyInfo.Name == "" {
		if message.Request.RequestingAgencyInfo == nil {
			message.Request.RequestingAgencyInfo = &iso18626.RequestingAgencyInfo{}
		}
		message.Request.RequestingAgencyInfo.Name = name
		if len(message.Request.RequestingAgencyInfo.Address) == 0 || message.Request.RequestingAgencyInfo.Address[0].PhysicalAddress == nil {
			if len(message.Request.RequestingAgencyInfo.Address) == 0 {
				message.Request.RequestingAgencyInfo.Address = []iso18626.Address{{PhysicalAddress: &address}}
			} else {
				message.Request.RequestingAgencyInfo.Address[0].PhysicalAddress = &address
			}
		}
	}
}

func populateDeliveryAddress(message *iso18626.ISO18626Message, name string, address iso18626.PhysicalAddress) {
	if len(message.Request.RequestedDeliveryInfo) == 0 {
		message.Request.RequestedDeliveryInfo = []iso18626.RequestedDeliveryInfo{{}}
	}
	if message.Request.RequestedDeliveryInfo[0].Address == nil {
		message.Request.RequestedDeliveryInfo[0].Address = &iso18626.Address{}
	}
	if message.Request.PatronInfo != nil && message.Request.PatronInfo.SendToPatron != nil &&
		*message.Request.PatronInfo.SendToPatron == iso18626.TypeYesNoY {
		if len(message.Request.PatronInfo.Address) > 0 && message.Request.RequestedDeliveryInfo[0].Address == nil {
			message.Request.RequestedDeliveryInfo[0].Address = &message.Request.PatronInfo.Address[0]
		}
	} else {
		if message.Request.RequestedDeliveryInfo[0].Address.PhysicalAddress == nil {
			message.Request.RequestedDeliveryInfo[0].Address.PhysicalAddress = &address
		}
	}
}

func populateSupplierInfo(message *iso18626.ISO18626Message, name string, agencyId iso18626.TypeAgencyId, address iso18626.PhysicalAddress) {
	if len(message.Request.SupplierInfo) == 0 {
		var sb strings.Builder
		shim.MarshalReturnLabel(&sb, name, &address)
		suppInfo := iso18626.SupplierInfo{
			SupplierDescription: sb.String(),
			SupplierCode:        &agencyId,
		}
		message.Request.SupplierInfo = []iso18626.SupplierInfo{suppInfo}
	}
}

func isDoNotSend(event events.Event) bool {
	if event.EventData.CustomData != nil {
		if forward, ok := event.EventData.CustomData["doNotSend"].(bool); ok && forward {
			return true
		}
	}
	return false
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

func (c *Iso18626Client) createMessageHeader(transaction ill_db.IllTransaction, sup *ill_db.LocatedSupplier, isRequestingMessage bool, brokerMode string) iso18626.Header {
	requesterSymbol := strings.Split(BrokerSymbol, ":")
	if !isRequestingMessage || brokerMode == string(extctx.BrokerModeTransparent) {
		requesterSymbol = strings.Split(transaction.RequesterSymbol.String, ":")
	}
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := strings.Split(BrokerSymbol, ":")
	if sup != nil && sup.SupplierSymbol != "" && (isRequestingMessage || brokerMode == string(extctx.BrokerModeTransparent)) {
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

func (c *Iso18626Client) validateReason(reason iso18626.TypeReasonForMessage, requesterAction string, prevStatus string) iso18626.TypeReasonForMessage {
	var expectedReason iso18626.TypeReasonForMessage
	switch requesterAction {
	case ill_db.RequestAction:
		if len(prevStatus) > 0 {
			expectedReason = iso18626.TypeReasonForMessageStatusChange
		} else {
			expectedReason = iso18626.TypeReasonForMessageRequestResponse
		}
	case string(iso18626.TypeActionStatusRequest):
		expectedReason = iso18626.TypeReasonForMessageStatusRequestResponse
	case string(iso18626.TypeActionNotification):
		expectedReason = ""
	case string(iso18626.TypeActionRenew):
		expectedReason = iso18626.TypeReasonForMessageRenewResponse
	case string(iso18626.TypeActionCancel):
		expectedReason = iso18626.TypeReasonForMessageCancelResponse
	default:
		expectedReason = iso18626.TypeReasonForMessageStatusChange
	}
	if expectedReason != "" {
		return expectedReason
	} else {
		return reason
	}
}

func (c *Iso18626Client) checkConfirmationError(isRequest bool, response *iso18626.ISO18626Message, defaultStatus events.EventStatus) events.EventStatus {
	if isRequest && response.RequestConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		return events.EventStatusProblem
	} else if !isRequest && response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		return events.EventStatusProblem
	}
	return defaultStatus
}

func (c *Iso18626Client) SendHttpPost(peer *ill_db.Peer, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	httpClient := httpclient.NewClient().
		WithMaxSize(int64(c.maxMsgSize)).
		WithHeaders("User-Agent", vcs.GetSignature())
	for k, v := range peer.HttpHeaders {
		httpClient.WithHeaders(k, v)
	}
	time.Sleep(c.sendDelay)
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

func getPeerNameAgencyIdAddress(peer *ill_db.Peer, symbol string) (string, iso18626.TypeAgencyId, iso18626.PhysicalAddress) {
	name := peer.Name
	agencyId := iso18626.TypeAgencyId{}
	if symbol != "" {
		name = fmt.Sprintf("%v (%v)", peer.Name, symbol)
		parts := strings.Split(symbol, ":")
		if len(parts) == 2 {
			agencyId.AgencyIdType = iso18626.TypeSchemeValuePair{Text: parts[0]}
			agencyId.AgencyIdValue = parts[1]
		}
	}
	address := iso18626.PhysicalAddress{}
	if listMap, ok := peer.CustomData["addresses"].([]any); ok && len(listMap) > 0 {
		for _, s := range listMap {
			if addressMap, castOk := s.(map[string]any); castOk {
				typeS, typeOk := addressMap["type"].(string)
				comp, compOk := addressMap["addressComponents"].([]any)
				if typeOk && compOk && typeS == "Shipping" {
					for _, c := range comp {
						if compMap, cCastOk := c.(map[string]any); cCastOk {
							part, partOk := compMap["value"].(string)
							pType, pTypeOk := compMap["type"].(string)
							if partOk && pTypeOk {
								switch pType {
								case "Thoroughfare":
									address.Line1 = part
								case "Locality":
									address.Locality = part
								case "AdministrativeArea":
									address.Region = &iso18626.TypeSchemeValuePair{
										Text: part,
									}
								case "PostalCode":
									address.PostalCode = part
								case "CountryCode":
									address.Country = &iso18626.TypeSchemeValuePair{
										Text: part,
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return name, agencyId, address
}
