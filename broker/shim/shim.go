package shim

import (
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

var NOTE_FIELD_SEP = utils.GetEnv("NOTE_FIELD_SEP", ", ")
var OFFERED_COSTS, _ = utils.GetEnvBool("OFFERED_COSTS", false)

const DELIVERY_ADDRESS_BEGIN = "#SHIP_TO#"
const DELIVERY_ADDRESS_END = "#ST_END#"
const RETURN_ADDRESS_BEGIN = "#RETURN_TO#"
const RETURN_ADDRESS_END = "#RT_END#"
const URL_PRE = "URL: "
const LOAN_CONDITION_PRE = "Condition: "
const COST_CONDITION_PRE = "Cost: "
const RESHARE_SUPPLIER_AWAITING_CONDITION = "#ReShareSupplierAwaitingConditionConfirmation#"
const ALMA_SUPPLIER_AWAITING_CONDITION = "Conditions pending approval, please respond `ACCEPT` or `REJECT`"
const RESHARE_ADD_LOAN_CONDITION = "#ReShareAddLoanCondition#"
const RESHARE_SUPPLIER_CONDITIONS_ASSUMED_AGREED = "#ReShareSupplierConditionsAssumedAgreed#"
const ALMA_SUPPLIER_CONDITIONS_ASSUMED_AGREED = "Supplier assumes approval of conditions unless `REJECT` is sent"
const ACCEPT = "ACCEPT"
const REJECT = "REJECT"
const RESHARE_LOAN_CONDITION_AGREE = "#ReShareLoanConditionAgreeResponse#"
const LOAN_CONDITION_OTHER = "other" //non-standard LC used by ReShare

var rsNoteRegexp = regexp.MustCompile(`#seq:[0-9]+#`)
var edgeNonWord = regexp.MustCompile(`^\W+|\W+$`)

type Iso18626Shim interface {
	ApplyToOutgoingRequest(message *iso18626.ISO18626Message) ([]byte, error)
	ApplyToIncomingResponse(bytes []byte, message *iso18626.ISO18626Message) error
	ApplyToIncomingRequest(message *iso18626.ISO18626Message, requester *ill_db.Peer, supplier *ill_db.LocatedSupplier) *iso18626.ISO18626Message
}

// factory method
func GetShim(vendor string) Iso18626Shim {
	var shim Iso18626Shim
	switch vendor {
	case string(directory.Alma):
		shim = new(Iso18626AlmaShim)
	case string(directory.ReShare):
		shim = new(Iso18626ReShareShim)
	default:
		shim = new(Iso18626DefaultShim)
	}
	return shim
}

type Iso18626DefaultShim struct {
}

func (i *Iso18626DefaultShim) ApplyToOutgoingRequest(message *iso18626.ISO18626Message) ([]byte, error) {
	return xml.Marshal(message)
}

func (i *Iso18626DefaultShim) ApplyToIncomingResponse(bytes []byte, message *iso18626.ISO18626Message) error {
	return xml.Unmarshal(bytes, message)
}

func (i *Iso18626DefaultShim) ApplyToIncomingRequest(message *iso18626.ISO18626Message, requester *ill_db.Peer, supplier *ill_db.LocatedSupplier) *iso18626.ISO18626Message {
	return message
}

type Iso18626AlmaShim struct {
	Iso18626DefaultShim
}

func (i *Iso18626AlmaShim) ApplyToIncomingRequest(message *iso18626.ISO18626Message, requester *ill_db.Peer, supplier *ill_db.LocatedSupplier) *iso18626.ISO18626Message {
	if message == nil {
		return message
	}
	copyMessage := *message
	if message.RequestingAgencyMessage != nil {
		copyRam := *message.RequestingAgencyMessage
		copyMessage.RequestingAgencyMessage = &copyRam
		i.fixRequesterConditionNote(copyMessage.RequestingAgencyMessage)
		if copyMessage.RequestingAgencyMessage.Action == iso18626.TypeActionCancel && supplier != nil {
			symbol := strings.SplitN(supplier.SupplierSymbol, ":", 2)
			copyMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text = symbol[0]
			copyMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue = symbol[1]
		}
	}
	return &copyMessage
}

func (i *Iso18626AlmaShim) ApplyToOutgoingRequest(message *iso18626.ISO18626Message) ([]byte, error) {
	if message != nil {
		if message.SupplyingAgencyMessage != nil {
			suppMsg := message.SupplyingAgencyMessage
			i.fixStatus(suppMsg)
			i.fixReasonForMessage(suppMsg)
			i.fixLoanCondition(suppMsg)
			i.transferOfferedCostsToDeliveryCosts(suppMsg)
			i.stripReShareSuppMsgSeqNote(suppMsg)
			i.humanizeReShareSupplierConditionNote(suppMsg)
			i.prependURLToSuppMsgNote(suppMsg)
			i.prependLoanConditionOrCostToNote(suppMsg)
			if suppMsg.StatusInfo.Status == iso18626.TypeStatusLoaned {
				i.appendReturnAddressToSuppMsgNote(suppMsg)
			}
			i.appendUnfilledStatusAndReasonUnfilled(suppMsg)
		}
		if message.Request != nil {
			request := message.Request
			i.fixServiceLevel(request)
			i.fixBibItemIds(request)
			i.fixBibRecIds(request)
			i.fixPublicationType(request)
			i.stripReShareReqSeqNote(request)
			i.appendMaxCostToReqNote(request)
			i.appendDeliveryAddressToReqNote(request)
			i.appendReturnAddressToReqNote(request)
		}
		if message.RequestingAgencyMessage != nil {
			reqMsg := message.RequestingAgencyMessage
			i.stripReShareReqMsgSeqNote(reqMsg)
			i.humanizeReShareRequesterNote(reqMsg)
		}
	}
	return xml.Marshal(message)
}

func (i *Iso18626AlmaShim) stripReShareSuppMsgSeqNote(suppMsg *iso18626.SupplyingAgencyMessage) {
	suppMsg.MessageInfo.Note = rsNoteRegexp.ReplaceAllString(suppMsg.MessageInfo.Note, "")
}

func (i *Iso18626AlmaShim) stripReShareReqMsgSeqNote(reqMsg *iso18626.RequestingAgencyMessage) {
	reqMsg.Note = rsNoteRegexp.ReplaceAllString(reqMsg.Note, "")
}

func (i *Iso18626AlmaShim) stripReShareReqSeqNote(request *iso18626.Request) {
	if request.ServiceInfo == nil {
		return
	}
	request.ServiceInfo.Note = rsNoteRegexp.ReplaceAllString(request.ServiceInfo.Note, "")
}

func (i *Iso18626AlmaShim) appendReturnAddressToSuppMsgNote(suppMsg *iso18626.SupplyingAgencyMessage) {
	if strings.Contains(suppMsg.MessageInfo.Note, RETURN_ADDRESS_BEGIN) {
		return
	}
	if suppMsg.ReturnInfo != nil {
		addr := suppMsg.ReturnInfo.PhysicalAddress
		var sb strings.Builder
		//retain original note
		if suppMsg.MessageInfo.Note != "" {
			sb.WriteString(suppMsg.MessageInfo.Note)
			sb.WriteString("\n")
		}
		MarshalReturnLabel(&sb, suppMsg.ReturnInfo.Name, addr)
		// put in the note
		suppMsg.MessageInfo.Note = sb.String()
	}
}

func (i *Iso18626AlmaShim) prependLoanConditionOrCostToNote(suppMsg *iso18626.SupplyingAgencyMessage) {
	condition := ""
	if suppMsg.DeliveryInfo != nil && suppMsg.DeliveryInfo.LoanCondition != nil {
		condition = suppMsg.DeliveryInfo.LoanCondition.Text
	}
	cost := i.MarshalCost(suppMsg.MessageInfo.OfferedCosts)
	sep := ""
	prependNote := ""
	origNote := suppMsg.MessageInfo.Note
	if len(condition) > 0 && !strings.Contains(origNote, LOAN_CONDITION_PRE) {
		prependNote = LOAN_CONDITION_PRE + condition
		sep = NOTE_FIELD_SEP
	}
	if len(cost) > 0 && !strings.Contains(origNote, COST_CONDITION_PRE) {
		prependNote = prependNote + sep + COST_CONDITION_PRE + cost
		sep = NOTE_FIELD_SEP
	}
	suppMsg.MessageInfo.Note = prependNote
	if len(origNote) > 0 {
		suppMsg.MessageInfo.Note = suppMsg.MessageInfo.Note + sep + origNote
	}
}

func (i *Iso18626AlmaShim) appendUnfilledStatusAndReasonUnfilled(suppMsg *iso18626.SupplyingAgencyMessage) {
	if suppMsg.StatusInfo.Status == iso18626.TypeStatusUnfilled &&
		suppMsg.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageNotification {
		if suppMsg.MessageInfo.Note == "" {
			suppMsg.MessageInfo.Note = "Status: Unfilled"
		} else {
			suppMsg.MessageInfo.Note += NOTE_FIELD_SEP + "Status: Unfilled"
		}
		if suppMsg.MessageInfo.ReasonUnfilled != nil {
			suppMsg.MessageInfo.Note += NOTE_FIELD_SEP + "Reason: " + suppMsg.MessageInfo.ReasonUnfilled.Text
		}
	}
}

func (i *Iso18626AlmaShim) transferOfferedCostsToDeliveryCosts(suppMsg *iso18626.SupplyingAgencyMessage) {
	//alma doesn't care about the delivery costs unless the status is Loaned or CopyCompleted
	if suppMsg.StatusInfo.Status != iso18626.TypeStatusLoaned && suppMsg.StatusInfo.Status != iso18626.TypeStatusCopyCompleted {
		return
	}
	if suppMsg.MessageInfo.OfferedCosts == nil {
		return
	}
	if suppMsg.DeliveryInfo == nil {
		suppMsg.DeliveryInfo = &iso18626.DeliveryInfo{}
	}
	if suppMsg.DeliveryInfo.DeliveryCosts == nil {
		suppMsg.DeliveryInfo.DeliveryCosts = suppMsg.MessageInfo.OfferedCosts
	}
}

func (*Iso18626AlmaShim) MarshalCost(tcost *iso18626.TypeCosts) string {
	cost := ""
	if tcost != nil {
		decimal := tcost.MonetaryValue
		cost = utils.FormatDecimal(decimal.Base, decimal.Exp)
		currencyCode := tcost.CurrencyCode.Text
		if len(cost) > 0 && len(currencyCode) > 0 {
			cost += " " + currencyCode
		}
	}
	return cost
}

func (i *Iso18626AlmaShim) prependURLToSuppMsgNote(suppMsg *iso18626.SupplyingAgencyMessage) {
	if suppMsg.DeliveryInfo != nil && suppMsg.DeliveryInfo.SentVia != nil &&
		suppMsg.DeliveryInfo.SentVia.Text == string(iso18626.SentViaUrl) &&
		!strings.Contains(suppMsg.MessageInfo.Note, URL_PRE) {
		url := suppMsg.DeliveryInfo.ItemId
		if suppMsg.MessageInfo.Note != "" {
			suppMsg.MessageInfo.Note = URL_PRE + url + NOTE_FIELD_SEP + suppMsg.MessageInfo.Note
		} else {
			suppMsg.MessageInfo.Note = URL_PRE + url
		}
	}
}

func (*Iso18626AlmaShim) fixStatus(suppMsg *iso18626.SupplyingAgencyMessage) {
	status := suppMsg.StatusInfo.Status
	// Alma does not support the status "ExpectToSupply" so we change it to "WillSupply"
	if status == iso18626.TypeStatusExpectToSupply {
		suppMsg.StatusInfo.Status = iso18626.TypeStatusWillSupply
		return
	}
	// note: Alma sets an empty status for its notifications so we may need to do the same here
	// ReShare on the other hand always sets status of Notifications to RequestReceived, so we may need to fix that the ReShare shim
}

func (*Iso18626AlmaShim) fixReasonForMessage(suppMsg *iso18626.SupplyingAgencyMessage) {
	reason := suppMsg.MessageInfo.ReasonForMessage
	if reason == iso18626.TypeReasonForMessageRequestResponse || reason == iso18626.TypeReasonForMessageStatusChange {
		status := suppMsg.StatusInfo.Status
		if status == iso18626.TypeStatusWillSupply {
			if suppMsg.DeliveryInfo != nil && suppMsg.DeliveryInfo.LoanCondition != nil &&
				suppMsg.DeliveryInfo.LoanCondition.Text != "" {
				//we append loan conditions to the note for Alma and need to change the reason so Alma shows the note
				suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageNotification
				return
			}
			if suppMsg.MessageInfo.OfferedCosts != nil &&
				suppMsg.MessageInfo.OfferedCosts.MonetaryValue.Base > 0 {
				//we append cost to the note for Alma and need to change the reason so Alma shows the note
				suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageNotification
				return
			}
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
			return
		}
		if status == iso18626.TypeStatusLoaned {
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
			return
		}
		if status == iso18626.TypeStatusLoanCompleted {
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
			return
		}
	}
}

func (i *Iso18626AlmaShim) fixLoanCondition(request *iso18626.SupplyingAgencyMessage) {
	if request.DeliveryInfo == nil || request.DeliveryInfo.LoanCondition == nil {
		return
	}
	lc, ok := iso18626.LoanConditionFromStringCI(request.DeliveryInfo.LoanCondition.Text)
	if ok {
		request.DeliveryInfo.LoanCondition.Text = string(lc)
	}
}

func (i *Iso18626AlmaShim) appendMaxCostToReqNote(request *iso18626.Request) {
	if request.ServiceInfo != nil && strings.Contains(request.ServiceInfo.Note, COST_CONDITION_PRE) {
		// already appended
		return
	}
	if request.BillingInfo != nil && request.BillingInfo.MaximumCosts != nil {
		tCost := request.BillingInfo.MaximumCosts
		if tCost.MonetaryValue.Base > 0 {
			cost := i.MarshalCost(tCost)

			if request.ServiceInfo == nil {
				request.ServiceInfo = new(iso18626.ServiceInfo)
			}
			if len(request.ServiceInfo.Note) > 0 {
				request.ServiceInfo.Note = request.ServiceInfo.Note + "\n"
			}
			request.ServiceInfo.Note = request.ServiceInfo.Note + COST_CONDITION_PRE + cost
		}
	}
}

func (i *Iso18626AlmaShim) appendDeliveryAddressToReqNote(request *iso18626.Request) {
	if request.ServiceInfo != nil && strings.Contains(request.ServiceInfo.Note, DELIVERY_ADDRESS_BEGIN) {
		return
	}
	if len(request.RequestedDeliveryInfo) > 0 {
		for _, deliveryInfo := range request.RequestedDeliveryInfo {
			if deliveryInfo.Address != nil {
				if deliveryInfo.Address.PhysicalAddress != nil {
					addr := deliveryInfo.Address.PhysicalAddress
					var sb strings.Builder
					//retain original note
					if request.ServiceInfo != nil {
						if request.ServiceInfo.Note != "" {
							sb.WriteString(request.ServiceInfo.Note)
							sb.WriteString("\n")
						}
					} else {
						request.ServiceInfo = new(iso18626.ServiceInfo)
					}
					requesterName := ""
					if request.RequestingAgencyInfo != nil {
						requesterName = request.RequestingAgencyInfo.Name
					}
					MarshalShipLabel(&sb, requesterName, addr)
					request.ServiceInfo.Note = sb.String()
					break
				}
			}
		}
	}
}

func (i *Iso18626AlmaShim) appendReturnAddressToReqNote(request *iso18626.Request) {
	if request.ServiceInfo != nil && strings.Contains(request.ServiceInfo.Note, RETURN_ADDRESS_BEGIN) {
		return
	}
	for _, suppInfo := range request.SupplierInfo {
		if strings.HasPrefix(suppInfo.SupplierDescription, RETURN_ADDRESS_BEGIN) {
			request.ServiceInfo.Note = request.ServiceInfo.Note + "\n" + suppInfo.SupplierDescription
			return
		}
	}
}

func (i *Iso18626AlmaShim) fixServiceLevel(request *iso18626.Request) {
	if request.ServiceInfo == nil || request.ServiceInfo.ServiceLevel == nil {
		return
	}
	serviceLevel, ok := iso18626.ServiceLevelFromStringCI(request.ServiceInfo.ServiceLevel.Text)
	if !ok {
		serviceLevel = iso18626.ServiceLevelStandard
	}
	request.ServiceInfo.ServiceLevel.Text = string(serviceLevel)
}

func (i *Iso18626AlmaShim) fixPublicationType(request *iso18626.Request) {
	if request.PublicationInfo == nil || request.PublicationInfo.PublicationType == nil {
		return
	}
	pubType, ok := iso18626.PublicationTypeFromStringCI(request.PublicationInfo.PublicationType.Text)
	if ok {
		request.PublicationInfo.PublicationType.Text = string(pubType)
	}
}

func (i *Iso18626AlmaShim) fixBibItemIds(request *iso18626.Request) {
	var bibItemIds []iso18626.BibliographicItemId
	for _, bibItemId := range request.BibliographicInfo.BibliographicItemId {
		val, err := iso18626.BibliographicItemIdCodeFromString(strings.ToUpper(bibItemId.BibliographicItemIdentifierCode.Text))
		if err == nil {
			bibItemIds = append(bibItemIds, iso18626.BibliographicItemId{
				BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
					Text: string(val),
				},
				BibliographicItemIdentifier: bibItemId.BibliographicItemIdentifier,
			})
		}
	}
	request.BibliographicInfo.BibliographicItemId = bibItemIds
}

func (i *Iso18626AlmaShim) fixBibRecIds(request *iso18626.Request) {
	var bibRecIds []iso18626.BibliographicRecordId
	for _, bibRecId := range request.BibliographicInfo.BibliographicRecordId {
		val, err := iso18626.BibliographicRecordIdCodeFromString(strings.ToUpper(bibRecId.BibliographicRecordIdentifierCode.Text))
		if err == nil {
			bibRecIds = append(bibRecIds, iso18626.BibliographicRecordId{
				BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{
					Text: string(val),
				},
				BibliographicRecordIdentifier: bibRecId.BibliographicRecordIdentifier,
			})
		}
	}
	if len(request.BibliographicInfo.SupplierUniqueRecordId) > 0 {
		bibRecIds = append(bibRecIds, iso18626.BibliographicRecordId{
			BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{
				Text: string(iso18626.BibliographicRecordIdCodeOCLC),
			},
			BibliographicRecordIdentifier: request.BibliographicInfo.SupplierUniqueRecordId,
		})
	}
	request.BibliographicInfo.BibliographicRecordId = bibRecIds
}

func MarshalShipLabel(sb *strings.Builder, name string, address *iso18626.PhysicalAddress) {
	sb.WriteString(DELIVERY_ADDRESS_BEGIN)
	sb.WriteString("\n")
	if name != "" {
		sb.WriteString(name)
		sb.WriteString("\n")
	}
	MarshalAddress(sb, address)
	sb.WriteString(DELIVERY_ADDRESS_END)
	sb.WriteString("\n")
}

func MarshalReturnLabel(sb *strings.Builder, name string, address *iso18626.PhysicalAddress) {
	sb.WriteString(RETURN_ADDRESS_BEGIN)
	sb.WriteString("\n")
	if name != "" {
		sb.WriteString(name)
		sb.WriteString("\n")
	}
	MarshalAddress(sb, address)
	sb.WriteString(RETURN_ADDRESS_END)
	sb.WriteString("\n")
}

func MarshalAddress(sb *strings.Builder, addr *iso18626.PhysicalAddress) {
	if addr.Line1 != "" {
		sb.WriteString(addr.Line1)
		sb.WriteString("\n")
	}
	if addr.Line2 != "" {
		sb.WriteString(addr.Line2)
		sb.WriteString("\n")
	}
	line3 := false
	if addr.Locality != "" {
		sb.WriteString(addr.Locality)
		line3 = true
	}
	if addr.Region != nil {
		if line3 {
			sb.WriteString(", ")
		}
		sb.WriteString(addr.Region.Text)
		line3 = true
	}
	if addr.PostalCode != "" {
		if line3 {
			sb.WriteString(", ")
		}
		sb.WriteString(addr.PostalCode)
		line3 = true
	}
	if line3 {
		sb.WriteString("\n")
	}
	if addr.Country != nil && len(addr.Country.Text) > 0 {
		sb.WriteString(addr.Country.Text)
		sb.WriteString("\n")
	}
}

func (i *Iso18626AlmaShim) humanizeReShareRequesterNote(ram *iso18626.RequestingAgencyMessage) {
	if strings.Contains(ram.Note, RESHARE_LOAN_CONDITION_AGREE) {
		note := strings.ReplaceAll(ram.Note, RESHARE_LOAN_CONDITION_AGREE, "")
		note = strings.TrimSpace(note)
		ram.Note = note
	}
}

func (i *Iso18626AlmaShim) humanizeReShareSupplierConditionNote(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage) {
	if strings.TrimSpace(supplyingAgencyMessage.MessageInfo.Note) == RESHARE_SUPPLIER_AWAITING_CONDITION {
		supplyingAgencyMessage.MessageInfo.Note = ALMA_SUPPLIER_AWAITING_CONDITION
		return
	}
	if strings.Contains(supplyingAgencyMessage.MessageInfo.Note, RESHARE_ADD_LOAN_CONDITION) {
		note := strings.ReplaceAll(supplyingAgencyMessage.MessageInfo.Note, RESHARE_ADD_LOAN_CONDITION, "")
		note = strings.TrimSpace(note)
		supplyingAgencyMessage.MessageInfo.Note = note
		return
	}
	if strings.TrimSpace(supplyingAgencyMessage.MessageInfo.Note) == RESHARE_SUPPLIER_CONDITIONS_ASSUMED_AGREED {
		supplyingAgencyMessage.MessageInfo.Note = ALMA_SUPPLIER_CONDITIONS_ASSUMED_AGREED
		return
	}
}

func (i *Iso18626AlmaShim) fixRequesterConditionNote(requestingAgencyMessage *iso18626.RequestingAgencyMessage) {
	if requestingAgencyMessage.Action == iso18626.TypeActionNotification {
		note := rsNoteRegexp.ReplaceAllString(requestingAgencyMessage.Note, "") //this is only needed to test human-notes from ReShare
		note = edgeNonWord.ReplaceAllString(note, "")
		if strings.EqualFold(note, ACCEPT) {
			requestingAgencyMessage.Note = RESHARE_LOAN_CONDITION_AGREE + requestingAgencyMessage.Note
		} else if strings.EqualFold(note, REJECT) {
			requestingAgencyMessage.Action = iso18626.TypeActionCancel
		}
	}
}

type Iso18626ReShareShim struct {
	Iso18626DefaultShim
}

func (i *Iso18626ReShareShim) ApplyToOutgoingRequest(message *iso18626.ISO18626Message) ([]byte, error) {
	if message.SupplyingAgencyMessage != nil {
		i.transferDeliveryCostsToOfferedCosts(message.SupplyingAgencyMessage)
	}
	return xml.Marshal(message)
}

func (i *Iso18626ReShareShim) transferDeliveryCostsToOfferedCosts(suppMsg *iso18626.SupplyingAgencyMessage) {
	if OFFERED_COSTS {
		if suppMsg.DeliveryInfo == nil || suppMsg.DeliveryInfo.DeliveryCosts == nil {
			return
		}
		if suppMsg.MessageInfo.OfferedCosts == nil {
			suppMsg.MessageInfo.OfferedCosts = suppMsg.DeliveryInfo.DeliveryCosts
			// also append a loan condition so reshare shows the cost as a condition
			if suppMsg.DeliveryInfo.LoanCondition == nil {
				suppMsg.DeliveryInfo.LoanCondition = &iso18626.TypeSchemeValuePair{
					Text: LOAN_CONDITION_OTHER,
				}
			}
		}
	}
}
