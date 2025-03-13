package app

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/illmock/role"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type supplierInfo struct {
	overdue           bool                  // overdue flag
	loaned            bool                  // if loaned
	supplierRequestId string                // supplier request Id
	requesterUrl      string                // requester URL
	presentResponse   bool                  // if it's first supplying message
	reasonRetry       *iso18626.ReasonRetry // used on retry

}

type Supplier struct {
	requests sync.Map
}

func (s *Supplier) getKey(header *iso18626.Header) string {
	return header.SupplyingAgencyId.AgencyIdValue + "/" + header.RequestingAgencyId.AgencyIdValue + "/" + header.RequestingAgencyRequestId
}

func (s *Supplier) load(header *iso18626.Header) *supplierInfo {
	v, ok := s.requests.Load(s.getKey(header))
	if !ok {
		return nil
	}
	return v.(*supplierInfo)
}

func (s *Supplier) store(header *iso18626.Header, info *supplierInfo) {
	s.requests.Store(s.getKey(header), info)
}

func (s *Supplier) delete(header *iso18626.Header) {
	s.requests.Delete(s.getKey(header))
}

func getScenarioForRequest(illRequest *iso18626.Request) string {
	scenario := illRequest.BibliographicInfo.SupplierUniqueRecordId
	var idx int = -1
	if strings.HasPrefix(scenario, "RETRY") {
		idx = strings.Index(scenario, "_")
	}
	// if request is already a retry, do not send retry again
	if illRequest.ServiceInfo != nil && illRequest.ServiceInfo.RequestType != nil &&
		*illRequest.ServiceInfo.RequestType == iso18626.TypeRequestTypeRetry {
		if idx > 0 {
			return scenario[idx+1:]
		}
	} else if idx > 0 {
		scenario = scenario[0:idx]
	}
	if scenario == "RETRY" {
		scenario = "RETRY:CostExceedsMaxCost"
	}
	return scenario
}

func (app *MockApp) handleSupplierRequest(illRequest *iso18626.Request, w http.ResponseWriter) {
	supplier := &app.supplier
	err := validateHeader(&illRequest.Header)
	if err != nil {
		app.handleRequestError(&illRequest.Header, role.Supplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	state := supplier.load(&illRequest.Header)
	if state != nil {
		app.handleRequestError(&illRequest.Header, role.Supplier, "RequestingAgencyRequestId already exists", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	overdue := false
	var status []iso18626.TypeStatus
	// should be able to parse the value and put any types into status...

	scenario := getScenarioForRequest(illRequest)
	var reasonRetry *iso18626.ReasonRetry

	switch scenario {
	case "RETRY:CostExceedsMaxCost":
		status = append(status, iso18626.TypeStatusRetryPossible)
		x := iso18626.ReasonRetryCostExceedsMaxCost
		reasonRetry = &x
	case "RETRY:OnLoan":
		status = append(status, iso18626.TypeStatusRetryPossible)
		x := iso18626.ReasonRetryOnLoan
		reasonRetry = &x
	case "RETRY:LoanCondition":
		status = append(status, iso18626.TypeStatusRetryPossible)
		x := iso18626.ReasonRetryLoanCondition
		reasonRetry = &x
	case "WILLSUPPLY_LOANED":
		status = append(status, iso18626.TypeStatusWillSupply, iso18626.TypeStatusLoaned)
	case "WILLSUPPLY_UNFILLED":
		status = append(status, iso18626.TypeStatusWillSupply, iso18626.TypeStatusUnfilled)
	case "UNFILLED":
		status = append(status, iso18626.TypeStatusUnfilled)
	case "LOANED":
		status = append(status, iso18626.TypeStatusLoaned)
	case "LOANED_OVERDUE":
		status = append(status, iso18626.TypeStatusLoaned)
		overdue = true
	case "WILLSUPPLY_LOANED_OVERDUE":
		status = append(status, iso18626.TypeStatusWillSupply, iso18626.TypeStatusLoaned)
		overdue = true
	case "ERROR":
		log.Warn("handleSupplierRequest ERROR")
		app.handleRequestError(&illRequest.Header, role.Supplier, "MOCK ERROR", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	case "HTTP-ERROR-400":
		http.Error(w, "MOCK HTTP-ERROR-400", http.StatusBadRequest)
		return
	case "HTTP-ERROR-500":
		http.Error(w, "MOCK HTTP-ERROR-500", http.StatusInternalServerError)
		return
	default:
		status = append(status, iso18626.TypeStatusUnfilled)
	}

	supplierInfo := &supplierInfo{
		supplierRequestId: uuid.NewString(),
		requesterUrl:      app.peerUrl,
		overdue:           overdue,
		presentResponse:   true,
		reasonRetry:       reasonRetry,
	}
	requestingAgencyInfo := illRequest.RequestingAgencyInfo
	if requestingAgencyInfo != nil {
		for _, address := range requestingAgencyInfo.Address {
			electronicAddress := address.ElectronicAddress
			if electronicAddress != nil {
				data := electronicAddress.ElectronicAddressData
				if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
					supplierInfo.requesterUrl = data
					break
				}
			}
		}
	}
	supplier.store(&illRequest.Header, supplierInfo)

	var resmsg = createRequestResponse(&illRequest.Header, iso18626.TypeMessageStatusOK, nil, nil)
	app.writeIso18626Response(resmsg, w, role.Supplier, &illRequest.Header)
	go app.sendSupplyingAgencyLater(&illRequest.Header, status)
}

func createSupplyingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = iso18626.NewIso18626MessageNS()
	msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	return msg
}

func (app *MockApp) sendSupplyingAgencyMessage(header *iso18626.Header, state *supplierInfo, msg *iso18626.Iso18626MessageNS) bool {
	msg.SupplyingAgencyMessage.Header = *header
	msg.SupplyingAgencyMessage.Header.SupplyingAgencyRequestId = state.supplierRequestId
	responseMsg, err := app.sendReceive(state.requesterUrl, msg, role.Supplier, header)
	if err != nil {
		log.Warn("sendSupplyingAgencyCancel", "error", err.Error())
		return false
	}
	if responseMsg.SupplyingAgencyMessageConfirmation == nil {
		log.Warn("sendSupplyingAgencyCancel did not receive SupplyingAgencyMessageConfirmation")
		return false
	}
	return true
}

func (app *MockApp) sendSupplyingAgencyLater(header *iso18626.Header, statusList []iso18626.TypeStatus) {
	time.Sleep(app.supplyDuration)

	supplier := &app.supplier
	state := supplier.load(header)
	if state == nil {
		log.Warn("sendSupplyingAgencyMessage no key", "key", supplier.getKey(header))
		return
	}
	msg := createSupplyingAgencyMessage()
	status := statusList[0]
	msg.SupplyingAgencyMessage.StatusInfo.Status = status
	if state.reasonRetry != nil {
		msg.SupplyingAgencyMessage.MessageInfo.ReasonRetry = &iso18626.TypeSchemeValuePair{Text: string(*state.reasonRetry)}
		switch *state.reasonRetry {
		case iso18626.ReasonRetryCostExceedsMaxCost:
			// CostExceedsMaxCost also puts a reason in ReasonUnfilled (bug really)
			msg.SupplyingAgencyMessage.MessageInfo.ReasonUnfilled = msg.SupplyingAgencyMessage.MessageInfo.ReasonRetry

			var amount utils.XSDDecimal
			amount.UnmarshalText([]byte("35.00"))

			msg.SupplyingAgencyMessage.MessageInfo.OfferedCosts = &iso18626.TypeCosts{
				CurrencyCode:  iso18626.TypeSchemeValuePair{Text: "USD"},
				MonetaryValue: amount,
			}
		case iso18626.ReasonRetryOnLoan:
			// the requester can retry now , basically!
			msg.SupplyingAgencyMessage.MessageInfo.RetryAfter = &utils.XSDDateTime{Time: time.Now()}
		case iso18626.ReasonRetryLoanCondition:
			msg.SupplyingAgencyMessage.DeliveryInfo = &iso18626.DeliveryInfo{}
			msg.SupplyingAgencyMessage.DeliveryInfo.LoanCondition = &iso18626.TypeSchemeValuePair{Text: "NoReproduction"}
		}
	}
	if state.presentResponse {
		state.presentResponse = false
		msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
	} else {
		msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
	}
	if status == iso18626.TypeStatusLoaned {
		state.loaned = true
	}
	if status == iso18626.TypeStatusLoanCompleted || status == iso18626.TypeStatusUnfilled || status == iso18626.TypeStatusRetryPossible {
		supplier.delete(header)
	}
	if app.sendSupplyingAgencyMessage(header, state, msg) {
		if len(statusList) > 1 {
			go app.sendSupplyingAgencyLater(header, statusList[1:])
		}
	}
}

func (app *MockApp) sendSupplyingAgencyOverdue(header *iso18626.Header, state *supplierInfo) {
	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
	msg.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusOverdue
	app.sendSupplyingAgencyMessage(header, state, msg)
}

func (app *MockApp) sendSupplyingAgencyRenew(header *iso18626.Header, state *supplierInfo) {
	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRenewResponse
	var answer iso18626.TypeYesNo = iso18626.TypeYesNoY
	msg.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusLoaned
	msg.SupplyingAgencyMessage.MessageInfo.AnswerYesNo = &answer
	app.sendSupplyingAgencyMessage(header, state, msg)
}

func (app *MockApp) sendSupplyingAgencyCancel(header *iso18626.Header, state *supplierInfo) {
	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageCancelResponse
	// cancel by default
	var answer iso18626.TypeYesNo = iso18626.TypeYesNoY
	var status iso18626.TypeStatus = iso18626.TypeStatusCancelled
	// check if already loaned
	if state.loaned {
		answer = iso18626.TypeYesNoN
		status = iso18626.TypeStatusLoaned
	}
	msg.SupplyingAgencyMessage.StatusInfo.Status = status
	msg.SupplyingAgencyMessage.MessageInfo.AnswerYesNo = &answer
	if status == iso18626.TypeStatusCancelled {
		supplier := &app.supplier
		supplier.delete(header)
	}
	app.sendSupplyingAgencyMessage(header, state, msg)
}

func (app *MockApp) handleRequestingAgencyMessageError(request *iso18626.RequestingAgencyMessage, role role.Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestingAgencyConfirmation(&request.Header, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	resmsg.RequestingAgencyMessageConfirmation.Action = &request.Action
	app.writeIso18626Response(resmsg, w, role, &request.Header)
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	requestingAgencyMessage := illMessage.RequestingAgencyMessage
	app.logIncomingReq(role.Supplier, &requestingAgencyMessage.Header, illMessage)
	err := validateHeader(&requestingAgencyMessage.Header)
	if err != nil {
		app.handleRequestingAgencyMessageError(requestingAgencyMessage, role.Supplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var resmsg = createRequestingAgencyConfirmation(&requestingAgencyMessage.Header, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestingAgencyMessageConfirmation.Action = &requestingAgencyMessage.Action
	app.writeIso18626Response(resmsg, w, role.Supplier, &requestingAgencyMessage.Header)

	header := &requestingAgencyMessage.Header
	supplier := &app.supplier
	state := supplier.load(header)
	if state == nil {
		log.Warn("sendSupplyingAgencyMessage no key", "key", supplier.getKey(header))
		return
	}
	switch requestingAgencyMessage.Action {
	case iso18626.TypeActionCancel:
		app.sendSupplyingAgencyCancel(header, state)
	case iso18626.TypeActionRenew:
		app.sendSupplyingAgencyRenew(header, state)
	case iso18626.TypeActionReceived:
		if state.overdue {
			state.overdue = false
			app.sendSupplyingAgencyOverdue(header, state)
		}
	case iso18626.TypeActionShippedReturn:
		go app.sendSupplyingAgencyLater(header, []iso18626.TypeStatus{iso18626.TypeStatusLoanCompleted})
	}
}
