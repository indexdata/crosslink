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

	var message = &iso18626.ISO18626Message{}
	locSupplier, peer, _ := c.getSupplier(ctx, illTrans)
	var defaultStatus *iso18626.TypeStatus
	if locSupplier == nil {
		dStatus := iso18626.TypeStatusUnfilled
		defaultStatus = &dStatus
	} else if locSupplier.LastStatus.String == string(iso18626.TypeStatusWillSupply) {
		fwStatus := illTrans.LastSupplierStatus.String
		if len(fwStatus) > 0 && fwStatus != string(iso18626.TypeStatusExpectToSupply) {
			resData.Note = "status WillSupply already communicated and will be ignored"
			return events.EventStatusSuccess, &resData
		}
	}

	statusInfo, statusErr := c.createStatusInfo(illTrans, locSupplier, defaultStatus)
	message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
		Header:      c.createMessageHeader(illTrans, peer, false),
		MessageInfo: c.createMessageInfo(),
		StatusInfo:  statusInfo,
	}
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
	if statusErr != nil {
		resData.EventError = &events.EventError{
			Message: "failed to get status",
			Cause:   statusErr.Error(),
		}
		ctx.Logger().Error("failed to get status", "error", statusErr)
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
	var action string
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
		action = illTrans.LastRequesterAction.String
		_, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
		return err
	})
	ctx.Logger().Info("CROSSLINK-83: updateSupplierStatus SAVE", "action", action)
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
	ctx.Logger().Info("CROSSLINK-83: createAndSendRequestOrRequestingAgencyMessage USE", "action", illTrans.LastRequesterAction.String, "isRequest", isRequest)
	var status = events.EventStatusSuccess
	var message = &iso18626.ISO18626Message{}
	var action string
	if isRequest {
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
		message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
			Header: c.createMessageHeader(illTrans, peer, true),
			Action: found,
			Note:   "",
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
		locsup, err := repo.GetSelectedSupplierForIllTransaction(ctx, id)
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

func (c *Iso18626Client) createStatusInfo(transaction ill_db.IllTransaction, supplier *ill_db.LocatedSupplier, defaultStatus *iso18626.TypeStatus) (iso18626.StatusInfo, error) {
	var status *iso18626.TypeStatus
	if supplier != nil {
		if s, ok := iso18626.StatusMap[supplier.LastStatus.String]; ok {
			status = &s
		}
	} else {
		status = defaultStatus
	}
	if status == nil {
		return iso18626.StatusInfo{}, errors.New("failed to resolve status for value")
	}
	return iso18626.StatusInfo{
		Status: *status,
		LastChange: utils.XSDDateTime{
			Time: transaction.Timestamp.Time,
		},
	}, nil
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
	httpClient := httpclient.NewClient()
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
