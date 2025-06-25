package client

import (
	"errors"
	"fmt"
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

// TODO this must be removed and saved from the initial request
var BrokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")
var appendSupplierInfo, _ = utils.GetEnvBool("SUPPLIER_INFO", true)
var appendRequestingAgencyInfo, _ = utils.GetEnvBool("REQ_AGENCY_INFO", true)
var appendReturnInfo, _ = utils.GetEnvBool("RETURN_INFO", true)
var prependVendor, _ = utils.GetEnvBool("VENDOR_INFO", true)

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
	firstMessage := true
	if locSupplier == nil {
		status = iso18626.TypeStatusUnfilled
	} else {
		if s, ok := iso18626.StatusMap[locSupplier.LastStatus.String]; ok {
			status = s
			firstMessage = false
		} else if !locSupplier.LastStatus.Valid {
			if requester.BrokerMode == string(extctx.BrokerModeTransparent) || requester.BrokerMode == string(extctx.BrokerModeTranslucent) {
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

	message.SupplyingAgencyMessage.Header = c.createMessageHeader(illTrans, locSupplier, false, requester.BrokerMode)
	reason := message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage
	message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = c.validateReason(reason, illTrans.LastRequesterAction.String, illTrans.LastSupplierStatus.String)
	message.SupplyingAgencyMessage.StatusInfo.Status = status
	message.SupplyingAgencyMessage.StatusInfo.LastChange = utils.XSDDateTime{Time: time.Now()}

	if status == iso18626.TypeStatusLoaned && appendReturnInfo {
		name, agencyId, address, _ := getPeerInfo(supplier, locSupplier.SupplierSymbol)
		populateReturnAddress(message, name, agencyId, address)
	}
	if prependVendor && firstMessage && supplier != nil {
		populateVendor(message.SupplyingAgencyMessage, supplier.Vendor)
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

func populateVendor(message *iso18626.SupplyingAgencyMessage, vendor string) {
	if message.MessageInfo.Note != "" {
		sep := ", "
		if strings.HasPrefix(message.MessageInfo.Note, "#") {
			sep = ""
		}
		message.MessageInfo.Note = "Vendor: " + vendor + sep + message.MessageInfo.Note
	} else {
		message.MessageInfo.Note = "Vendor: " + vendor
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
		requesterName, _, deliveryAddress, email := getPeerInfo(&requester, illTrans.RequesterSymbol.String)
		if appendRequestingAgencyInfo {
			populateRequesterInfo(message, requesterName, deliveryAddress, email)
		}
		populateDeliveryAddress(message, deliveryAddress, email)
		if appendSupplierInfo {
			supplierName, suppAgencyId, supplierAddress, _ := getPeerInfo(supplier, selected.SupplierSymbol)
			populateSupplierInfo(message, supplierName, suppAgencyId, supplierAddress)
		}
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

func populateRequesterInfo(message *iso18626.ISO18626Message, name string, address iso18626.PhysicalAddress, email iso18626.ElectronicAddress) {
	if message.Request.RequestingAgencyInfo == nil || message.Request.RequestingAgencyInfo.Name == "" {
		if message.Request.RequestingAgencyInfo == nil {
			message.Request.RequestingAgencyInfo = &iso18626.RequestingAgencyInfo{}
		}
		message.Request.RequestingAgencyInfo.Name = name
		var hasAddress, hasEmail bool
		for _, addr := range message.Request.RequestingAgencyInfo.Address {
			if addr.PhysicalAddress != nil && len(addr.PhysicalAddress.Line1) > 0 {
				hasAddress = true
			}
			if addr.ElectronicAddress != nil && len(addr.ElectronicAddress.ElectronicAddressData) > 0 {
				hasEmail = true
			}
		}
		if !hasAddress && address.Line1 != "" {
			message.Request.RequestingAgencyInfo.Address = append(message.Request.RequestingAgencyInfo.Address, iso18626.Address{
				PhysicalAddress: &address,
			})
		}
		if !hasEmail && email.ElectronicAddressData != "" {
			message.Request.RequestingAgencyInfo.Address = append(message.Request.RequestingAgencyInfo.Address, iso18626.Address{
				ElectronicAddress: &email,
			})
		}
	}
}

func populateDeliveryAddress(message *iso18626.ISO18626Message, address iso18626.PhysicalAddress, email iso18626.ElectronicAddress) {
	var hasAddress, hasEmail bool
	for _, di := range message.Request.RequestedDeliveryInfo {
		if di.Address != nil {
			addr := di.Address
			if addr.PhysicalAddress != nil && len(addr.PhysicalAddress.Line1) > 0 {
				hasAddress = true
			}
			if addr.ElectronicAddress != nil && len(addr.ElectronicAddress.ElectronicAddressData) > 0 {
				hasEmail = true
			}
		}
	}
	//send to patron
	if message.Request.PatronInfo != nil && message.Request.PatronInfo.SendToPatron != nil &&
		*message.Request.PatronInfo.SendToPatron == iso18626.TypeYesNoY {
		address = iso18626.PhysicalAddress{}
		email = iso18626.ElectronicAddress{}
		for _, addr := range message.Request.PatronInfo.Address {
			if addr.PhysicalAddress != nil {
				address = *addr.PhysicalAddress
			}
			if addr.ElectronicAddress != nil {
				email = *addr.ElectronicAddress
			}
		}
	}
	if !hasAddress && address.Line1 != "" {
		message.Request.RequestedDeliveryInfo = append(message.Request.RequestedDeliveryInfo, iso18626.RequestedDeliveryInfo{
			Address: &iso18626.Address{
				PhysicalAddress: &address,
			},
		})
	}
	if !hasEmail && email.ElectronicAddressData != "" {
		message.Request.RequestedDeliveryInfo = append(message.Request.RequestedDeliveryInfo, iso18626.RequestedDeliveryInfo{
			Address: &iso18626.Address{
				ElectronicAddress: &email,
			},
		})
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
	requesterSymbol := strings.SplitN(BrokerSymbol, ":", 2)
	if !isRequestingMessage || brokerMode == string(extctx.BrokerModeTransparent) {
		requesterSymbol = strings.SplitN(transaction.RequesterSymbol.String, ":", 2)
	}
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := strings.SplitN(BrokerSymbol, ":", 2)
	if sup != nil && sup.SupplierSymbol != "" && (isRequestingMessage || brokerMode == string(extctx.BrokerModeTransparent)) {
		supplierSymbol = strings.SplitN(sup.SupplierSymbol, ":", 2)
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
	if reason == iso18626.TypeReasonForMessageNotification {
		return reason
	}
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

func getPeerInfo(peer *ill_db.Peer, symbol string) (string, iso18626.TypeAgencyId, iso18626.PhysicalAddress, iso18626.ElectronicAddress) {
	name := peer.Name
	agencyId := iso18626.TypeAgencyId{}
	if symbol != "" {
		name = fmt.Sprintf("%v (%v)", peer.Name, symbol)
		parts := strings.SplitN(symbol, ":", 2)
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
	email := iso18626.ElectronicAddress{}
	if listMap, ok := peer.CustomData["email"].(string); ok && len(listMap) > 0 {
		email.ElectronicAddressData = listMap
		email.ElectronicAddressType = iso18626.TypeSchemeValuePair{
			Text: string(iso18626.ElectronicAddressTypeEmail),
		}
	}
	return name, agencyId, address, email
}
