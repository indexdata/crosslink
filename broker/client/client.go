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

const CLIENT_COMP = "iso18626_client"

const FailedToReadTransaction = "failed to read ILL transaction"
const FailedToSendMessage = "failed to send ISO18626 message"
const FailedToGetSupplier = "failed to get supplier"
const FailedToGetRequester = "failed to get requester"
const FailedToGetTrCtx = "failed to get transaction context"
const FailedToUpdateSupplierStatus = "failed to update supplier status"
const FailedToResolveStatus = "failed to resolve status for value: %s"
const BrokerDoesNotSendInThisMode = "broker does not send %s in mode %s"
const InvalidState = "invalid state"
const FailedToDetermineTarget = "failed to determine target"

const KindConfirmationError = "confirmation-error"

const BrokerInfoStatus = iso18626.TypeStatusExpectToSupply

var brokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")
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
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(CLIENT_COMP))
	_, _ = c.eventBus.ProcessTask(ctx, event, c.createAndSendSupplyingAgencyMessage)
}

func (c *Iso18626Client) MessageSupplier(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(CLIENT_COMP))
	_, _ = c.eventBus.ProcessTask(ctx, event, c.createAndSendRequestOrRequestingAgencyMessage)
}

func (c *Iso18626Client) getSkippedSupplierAndPeer(ctx extctx.ExtendedContext, illTransId, symbol string) (*ill_db.LocatedSupplier, *ill_db.Peer, error) {
	skipped, err := c.illRepo.GetLocatedSupplierByIllTransactionAndSymbol(ctx, illTransId, symbol)
	if err != nil {
		return nil, nil, err
	}
	if skipped.SupplierStatus != ill_db.SupplierStateSkippedPg {
		return nil, nil, fmt.Errorf("supplier %s is not in skipped state", symbol)
	}
	skippedPeer, err := c.illRepo.GetPeerById(ctx, skipped.SupplierID)
	if err != nil {
		return nil, nil, err
	}
	return &skipped, &skippedPeer, nil
}

func isNotification(msg *iso18626.ISO18626Message) bool {
	return msg != nil && msg.SupplyingAgencyMessage != nil &&
		msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageNotification
}

func getSymbol(msg *iso18626.ISO18626Message) string {
	symbol := ""
	if msg != nil && msg.SupplyingAgencyMessage != nil {
		symbol = msg.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" +
			msg.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
	}
	return symbol
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
		sep := shim.NOTE_FIELD_SEP
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

func (c *Iso18626Client) getSelectedSupplierAndPeer(ctx extctx.ExtendedContext, transaction ill_db.IllTransaction) (*ill_db.LocatedSupplier, *ill_db.Peer, error) {
	selectedSupplier, err := c.illRepo.GetSelectedSupplierForIllTransaction(ctx, transaction.ID)
	if err != nil {
		return nil, nil, err
	}
	peer, err := c.illRepo.GetPeerById(ctx, selectedSupplier.SupplierID)
	if err != nil {
		return nil, nil, err
	}
	return &selectedSupplier, &peer, nil
}

func createMessageHeader(transaction ill_db.IllTransaction, sup *ill_db.LocatedSupplier, isRequestingMessage bool, brokerMode string) iso18626.Header {
	requesterSymbol := strings.SplitN(brokerSymbol, ":", 2)
	if !isRequestingMessage || brokerMode == string(extctx.BrokerModeTransparent) {
		requesterSymbol = strings.SplitN(transaction.RequesterSymbol.String, ":", 2)
	}
	if len(requesterSymbol) < 2 {
		requesterSymbol = append(requesterSymbol, "")
	}
	supplierSymbol := strings.SplitN(brokerSymbol, ":", 2)
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

// suppliers like Alma often send a wrong reason so we try to guess the correct reason based on the requester action and previous status
func guessReason(reason iso18626.TypeReasonForMessage, requesterAction string, prevStatus string) iso18626.TypeReasonForMessage {
	// notification is a special case where we don't try to guess the reason
	if reason == iso18626.TypeReasonForMessageNotification {
		return reason
	}
	var expectedReason iso18626.TypeReasonForMessage
	switch requesterAction {
	case string(iso18626.TypeActionStatusRequest):
		expectedReason = iso18626.TypeReasonForMessageStatusRequestResponse
	case string(iso18626.TypeActionRenew):
		expectedReason = iso18626.TypeReasonForMessageRenewResponse
	case string(iso18626.TypeActionCancel):
		expectedReason = iso18626.TypeReasonForMessageCancelResponse
	default: //for everything else we guess we check if there is a previous status
		if len(prevStatus) > 0 {
			expectedReason = iso18626.TypeReasonForMessageStatusChange
		} else {
			expectedReason = iso18626.TypeReasonForMessageRequestResponse
		}
	}
	return expectedReason
}

func (c *Iso18626Client) checkConfirmationError(ctx extctx.ExtendedContext, response *iso18626.ISO18626Message, defaultStatus events.EventStatus, result *events.EventResult) events.EventStatus {
	status := defaultStatus
	if response.RequestConfirmation != nil &&
		response.RequestConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		msg := "request confirmation error"
		status = events.EventStatusProblem
		result.Problem = &events.Problem{
			Kind:    KindConfirmationError,
			Details: msg,
		}
		var errorType, errorValue string
		if response.RequestConfirmation.ErrorData != nil {
			errorType = string(response.RequestConfirmation.ErrorData.ErrorType)
			errorValue = response.RequestConfirmation.ErrorData.ErrorValue
		}
		var requesterSymbol string
		if response.RequestConfirmation.ConfirmationHeader.RequestingAgencyId != nil {
			requesterSymbol = response.RequestConfirmation.ConfirmationHeader.RequestingAgencyId.AgencyIdValue
		}
		var supplierSymbol string
		if response.RequestConfirmation.ConfirmationHeader.SupplyingAgencyId != nil {
			supplierSymbol = response.RequestConfirmation.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue
		}
		ctx.Logger().Warn(msg,
			"errorType", errorType,
			"errorValue", errorValue,
			"requesterSymbol", requesterSymbol,
			"supplierSymbol", supplierSymbol,
			"requesterRequestId", response.RequestConfirmation.ConfirmationHeader.RequestingAgencyRequestId)
	} else if response.RequestingAgencyMessageConfirmation != nil &&
		response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		msg := "requester message confirmation error"
		status = events.EventStatusProblem
		result.Problem = &events.Problem{
			Kind:    KindConfirmationError,
			Details: msg,
		}
		var errorType, errorValue string
		if response.RequestingAgencyMessageConfirmation.ErrorData != nil {
			errorType = string(response.RequestingAgencyMessageConfirmation.ErrorData.ErrorType)
			errorValue = response.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue
		}
		var requesterSymbol string
		if response.RequestingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyId != nil {
			requesterSymbol = response.RequestingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyId.AgencyIdValue
		}
		var supplierSymbol string
		if response.RequestingAgencyMessageConfirmation.ConfirmationHeader.SupplyingAgencyId != nil {
			supplierSymbol = response.RequestingAgencyMessageConfirmation.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue
		}
		var action iso18626.TypeAction
		if response.RequestingAgencyMessageConfirmation.Action != nil {
			action = *response.RequestingAgencyMessageConfirmation.Action
		}
		ctx.Logger().Warn(msg,
			"errorType", errorType,
			"errorValue", errorValue,
			"requesterSymbol", requesterSymbol,
			"supplierSymbol", supplierSymbol,
			"requesterRequestId", response.RequestingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyRequestId,
			"action", action)
	} else if response.SupplyingAgencyMessageConfirmation != nil &&
		response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus == iso18626.TypeMessageStatusERROR {
		msg := "supplier message confirmation error"
		status = events.EventStatusProblem
		result.Problem = &events.Problem{
			Kind:    KindConfirmationError,
			Details: msg,
		}
		var errorType, errorValue string
		if response.SupplyingAgencyMessageConfirmation.ErrorData != nil {
			errorType = string(response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorType)
			errorValue = response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue
		}
		var requesterSymbol string
		if response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyId != nil {
			requesterSymbol = response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyId.AgencyIdValue
		}
		var supplierSymbol string
		if response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.SupplyingAgencyId != nil {
			supplierSymbol = response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue
		}
		var reason iso18626.TypeReasonForMessage
		if response.SupplyingAgencyMessageConfirmation.ReasonForMessage != nil {
			reason = *response.SupplyingAgencyMessageConfirmation.ReasonForMessage
		}
		ctx.Logger().Warn(msg,
			"errorType", errorType,
			"errorValue", errorValue,
			"requesterSymbol", requesterSymbol,
			"supplierSymbol", supplierSymbol,
			"requesterRequestId", response.SupplyingAgencyMessageConfirmation.ConfirmationHeader.RequestingAgencyRequestId,
			"reason", reason)
	}
	return status
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

type transactionContext struct {
	transaction      *ill_db.IllTransaction
	requester        *ill_db.Peer
	selectedSupplier *ill_db.LocatedSupplier
	selectedPeer     *ill_db.Peer
	event            events.Event
}

func (c *Iso18626Client) readTransactionContext(ctx extctx.ExtendedContext, event events.Event, supplierMandatory bool) (transactionContext, error) {
	transCtx := transactionContext{
		event: event,
	}
	illTrans, err := c.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		return transCtx, fmt.Errorf("%s: %w", FailedToReadTransaction, err)
	} else {
		transCtx.transaction = &illTrans
	}
	requester, err := c.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		return transCtx, fmt.Errorf("%s: %w", FailedToGetRequester, err)
	} else {
		transCtx.requester = &requester
	}
	selected, supplier, err := c.getSelectedSupplierAndPeer(ctx, illTrans)
	if err != nil && supplierMandatory {
		return transCtx, fmt.Errorf("%s: %w", FailedToGetSupplier, err)
	} else {
		transCtx.selectedSupplier = selected
		transCtx.selectedPeer = supplier
	}
	return transCtx, nil
}

func (c *Iso18626Client) createAndSendSupplyingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	trCtx, err := c.readTransactionContext(ctx, event, false)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, FailedToGetTrCtx, err)
	}

	msgTarget, err := c.determineMessageTarget(ctx, trCtx)
	if err != nil {
		if msgTarget != nil && msgTarget.problemDetails != nil {
			return events.LogProblemAndReturnResult(ctx, InvalidState, msgTarget.problemDetails.message, nil)
		}
		return events.LogErrorAndReturnResult(ctx, FailedToDetermineTarget, err)
	}

	if msgTarget.status == "" {
		resData := events.EventResult{}
		resData.Note = msgTarget.note
		return events.EventStatusSuccess, &resData
	}

	message := buildSupplyingAgencyMessage(trCtx, msgTarget)

	return c.sendAndUpdateStatus(ctx, trCtx, message)
}

type messageTarget struct {
	supplier       *ill_db.LocatedSupplier
	peer           *ill_db.Peer
	status         iso18626.TypeStatus
	firstMessage   bool
	note           string
	problemDetails *struct{ message string }
}

func (c *Iso18626Client) determineMessageTarget(ctx extctx.ExtendedContext, trCtx transactionContext) (*messageTarget, error) {
	msgSupplierSymbol := getSymbol(trCtx.event.EventData.IncomingMessage)
	isNotification := isNotification(trCtx.event.EventData.IncomingMessage)
	if trCtx.selectedSupplier == nil {
		return c.handleNoSelectedSupplier(ctx, trCtx, msgSupplierSymbol, isNotification)
	}

	if isNotification && msgSupplierSymbol != "" && msgSupplierSymbol != trCtx.selectedSupplier.SupplierSymbol {
		return c.handleSkippedSupplierNotification(ctx, trCtx, msgSupplierSymbol)
	}

	return handleSelectedSupplier(trCtx)
}

func (c *Iso18626Client) handleNoSelectedSupplier(ctx extctx.ExtendedContext, trCtx transactionContext, msgSupplierSymbol string, isNotification bool) (*messageTarget, error) {
	if isNotification && msgSupplierSymbol != "" {
		return c.handleSkippedSupplierNotification(ctx, trCtx, msgSupplierSymbol)
	}

	return &messageTarget{status: iso18626.TypeStatusUnfilled, firstMessage: false}, nil
}

func (c *Iso18626Client) handleSkippedSupplierNotification(ctx extctx.ExtendedContext, trCtx transactionContext, msgSupplierSymbol string) (*messageTarget, error) {
	skipped, skippedPeer, err := c.getSkippedSupplierAndPeer(ctx, trCtx.transaction.ID, msgSupplierSymbol)
	if err != nil {
		return nil, err
	}

	if trCtx.requester.BrokerMode == string(extctx.BrokerModeTransparent) {
		return &messageTarget{
			supplier:     skipped,
			peer:         skippedPeer,
			status:       iso18626.TypeStatus(trCtx.transaction.LastSupplierStatus.String),
			firstMessage: false,
		}, nil
	}

	return &messageTarget{
		problemDetails: &struct{ message string }{
			message: fmt.Sprintf("ignored notification from skipped supplier %s due to requester mode %s", msgSupplierSymbol, trCtx.requester.BrokerMode),
		},
	}, nil
}

func handleSelectedSupplier(trCtx transactionContext) (*messageTarget, error) {
	lastReceivedStatus := trCtx.selectedSupplier.LastStatus
	if s, ok := iso18626.StatusMap[lastReceivedStatus.String]; ok {
		return &messageTarget{supplier: trCtx.selectedSupplier, peer: trCtx.selectedPeer, status: s, firstMessage: false}, nil
	}

	if !lastReceivedStatus.Valid {
		if trCtx.event.EventData.IncomingMessage == nil {
			if trCtx.requester.BrokerMode == string(extctx.BrokerModeTransparent) || trCtx.requester.BrokerMode == string(extctx.BrokerModeTranslucent) {
				return &messageTarget{supplier: trCtx.selectedSupplier, status: BrokerInfoStatus, firstMessage: true}, nil
			}
			return &messageTarget{note: fmt.Sprintf(BrokerDoesNotSendInThisMode, BrokerInfoStatus, trCtx.requester.BrokerMode)}, nil
		}
		return &messageTarget{supplier: trCtx.selectedSupplier, status: BrokerInfoStatus, firstMessage: true}, nil
	}

	return nil, fmt.Errorf(FailedToResolveStatus, lastReceivedStatus.String)
}

func buildSupplyingAgencyMessage(trCtx transactionContext, target *messageTarget) *iso18626.ISO18626Message {
	var message *iso18626.ISO18626Message
	if trCtx.event.EventData.IncomingMessage != nil && trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage != nil {
		message = trCtx.event.EventData.IncomingMessage
	} else {
		message = &iso18626.ISO18626Message{
			SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{},
		}
	}

	sam := message.SupplyingAgencyMessage
	sam.Header = createMessageHeader(*trCtx.transaction, target.supplier, false, trCtx.requester.BrokerMode)
	reason := sam.MessageInfo.ReasonForMessage
	sam.MessageInfo.ReasonForMessage = guessReason(reason, trCtx.transaction.LastRequesterAction.String, trCtx.transaction.LastSupplierStatus.String)
	sam.StatusInfo.Status = target.status
	sam.StatusInfo.LastChange = utils.XSDDateTime{Time: time.Now()}

	if target.status == iso18626.TypeStatusLoaned && appendReturnInfo {
		name, agencyId, address, _ := getPeerInfo(target.peer, target.supplier.SupplierSymbol)
		populateReturnAddress(message, name, agencyId, address)
	}
	if prependVendor && target.firstMessage && target.peer != nil && target.peer.Vendor != trCtx.requester.Vendor {
		populateVendor(sam, target.peer.Vendor)
	}

	return message
}

func (c *Iso18626Client) sendAndUpdateStatus(ctx extctx.ExtendedContext, trCtx transactionContext, message *iso18626.ISO18626Message) (events.EventStatus, *events.EventResult) {
	resData := &events.EventResult{}
	resData.OutgoingMessage = message

	if isDoNotSend(trCtx.event) {
		resData.CustomData = map[string]any{"doNotSend": true}
		resData.OutgoingMessage = nil
	} else {
		response, err := c.SendHttpPost(trCtx.requester, message)
		if response != nil {
			resData.IncomingMessage = response
		}
		if err != nil {
			var httpErr *httpclient.HttpError
			if errors.As(err, &httpErr) {
				resData.HttpFailure = httpErr
			}
			return events.LogErrorAndReturnExistingResult(ctx, FailedToSendMessage, err, resData)
		}
		if status := c.checkConfirmationError(ctx, response, events.EventStatusSuccess, resData); status != events.EventStatusSuccess {
			return status, resData
		}
	}

	err := c.updateSupplierStatus(ctx, trCtx.event.IllTransactionID, string(message.SupplyingAgencyMessage.StatusInfo.Status))
	if err != nil {
		return events.LogErrorAndReturnExistingResult(ctx, FailedToUpdateSupplierStatus, err, resData)
	}

	return events.EventStatusSuccess, resData
}

func (c *Iso18626Client) createAndSendRequestOrRequestingAgencyMessage(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	trCtx, err := c.readTransactionContext(ctx, event, true)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, FailedToGetTrCtx, err)
	}
	// if requester sends a message (e.g notification) to supplier and then a new supplier is selected,
	// the action on the transaction is not relevant and we need to look at the new supplier's last action
	// however, if requester sends a retry request it is captured on the transaction
	var isRequest = trCtx.selectedSupplier.LastAction.String == "" || trCtx.transaction.LastRequesterAction.String == ill_db.RequestAction
	var message *iso18626.ISO18626Message
	var action string
	if isRequest {
		message, action = createRequestMessage(trCtx)
	} else {
		errorMessage := ""
		message, action, errorMessage = createRequestingAgencyMessage(trCtx)
		if errorMessage != "" {
			return events.LogErrorAndReturnResult(ctx, errorMessage, nil)
		}
	}
	return c.sendAndUpdateSupplier(ctx, trCtx, message, action)
}

func createRequestMessage(trCtx transactionContext) (*iso18626.ISO18626Message, string) {
	var message = &iso18626.ISO18626Message{}
	message.Request = &iso18626.Request{
		Header:                createMessageHeader(*trCtx.transaction, trCtx.selectedSupplier, true, trCtx.selectedPeer.BrokerMode),
		BibliographicInfo:     trCtx.transaction.IllTransactionData.BibliographicInfo,
		PublicationInfo:       trCtx.transaction.IllTransactionData.PublicationInfo,
		ServiceInfo:           trCtx.transaction.IllTransactionData.ServiceInfo,
		SupplierInfo:          trCtx.transaction.IllTransactionData.SupplierInfo,
		RequestedDeliveryInfo: trCtx.transaction.IllTransactionData.RequestedDeliveryInfo,
		PatronInfo:            trCtx.transaction.IllTransactionData.PatronInfo,
		BillingInfo:           trCtx.transaction.IllTransactionData.BillingInfo,
		RequestingAgencyInfo:  trCtx.transaction.IllTransactionData.RequestingAgencyInfo,
	}
	message.Request.BibliographicInfo.SupplierUniqueRecordId = trCtx.selectedSupplier.LocalID.String
	requesterName, _, deliveryAddress, email := getPeerInfo(trCtx.requester, trCtx.transaction.RequesterSymbol.String)
	if appendRequestingAgencyInfo {
		populateRequesterInfo(message, requesterName, deliveryAddress, email)
	}
	populateDeliveryAddress(message, deliveryAddress, email)
	if appendSupplierInfo {
		supplierName, suppAgencyId, supplierAddress, _ := getPeerInfo(trCtx.selectedPeer, trCtx.selectedSupplier.SupplierSymbol)
		populateSupplierInfo(message, supplierName, suppAgencyId, supplierAddress)
	}
	return message, ill_db.RequestAction
}

func createRequestingAgencyMessage(trCtx transactionContext) (*iso18626.ISO18626Message, string, string) {
	var message = &iso18626.ISO18626Message{}
	found, ok := iso18626.ActionMap[trCtx.transaction.LastRequesterAction.String]
	if !ok {
		return message, "", "failed to resolve action for value: " + trCtx.transaction.LastRequesterAction.String
	}
	var note = ""
	if trCtx.event.EventData.IncomingMessage != nil && trCtx.event.EventData.IncomingMessage.RequestingAgencyMessage != nil {
		note = trCtx.event.EventData.IncomingMessage.RequestingAgencyMessage.Note
	}
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Header: createMessageHeader(*trCtx.transaction, trCtx.selectedSupplier, true, trCtx.selectedPeer.BrokerMode),
		Action: found,
		Note:   note,
	}
	return message, string(found), ""
}

func (c *Iso18626Client) sendAndUpdateSupplier(ctx extctx.ExtendedContext, trCtx transactionContext, message *iso18626.ISO18626Message, action string) (events.EventStatus, *events.EventResult) {
	eventStatus := events.EventStatusSuccess
	resData := events.EventResult{}
	resData.OutgoingMessage = message
	if !isDoNotSend(trCtx.event) {
		response, err := c.SendHttpPost(trCtx.selectedPeer, message)
		if response != nil {
			resData.IncomingMessage = response
		}
		if err != nil {
			var httpErr *httpclient.HttpError
			if errors.As(err, &httpErr) {
				resData.HttpFailure = httpErr
			}
			return events.LogErrorAndReturnExistingResult(ctx, FailedToSendMessage, err, &resData)
		} else {
			eventStatus = c.checkConfirmationError(ctx, response, eventStatus, &resData)
		}
	} else {
		if resData.CustomData == nil {
			resData.CustomData = map[string]any{}
		}
		resData.CustomData["doNotSend"] = true
		resData.OutgoingMessage = nil
	}
	// check for status == EvenStatusError and NOT save??
	err := c.updateSelectedSupplierAction(ctx, trCtx.transaction.ID, action)
	if err != nil {
		return events.LogErrorAndReturnExistingResult(ctx, FailedToUpdateSupplierStatus, err, &resData)
	}
	return eventStatus, &resData
}
