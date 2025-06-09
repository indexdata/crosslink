package shim

import (
	"encoding/xml"
	"github.com/indexdata/crosslink/broker/common"
	"regexp"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
)

const DELIVERY_ADDRESS_BEGIN = "#SHIP_TO#"
const DELIVERY_ADDRESS_END = "#ST_END#"
const RETURN_ADDRESS_BEGIN = "#RETURN_TO#"
const RETURN_ADDRESS_END = "#RT_END#"
const SUPPLIER_AWAITING_CONDITION = "#ReShareSupplierAwaitingConditionConfirmation#"
const LOAN_CONDITION_AGREE = "#ReShareLoanConditionAgreeResponse#"
const ACCEPT = "ACCEPT"
const REJECT = "REJECT"

var rsNoteRegexp = regexp.MustCompile(`#seq:[0-9]+#`)

type Iso18626Shim interface {
	ApplyToOutgoing(message *iso18626.ISO18626Message) ([]byte, error)
	ApplyToIncoming(bytes []byte, message *iso18626.ISO18626Message) error
}

// factory method
func GetShim(vendor string) Iso18626Shim {
	var shim Iso18626Shim
	switch vendor {
	case string(common.VendorAlma):
		shim = new(Iso18626AlmaShim)
	case string(common.VendorReShare):
		shim = new(Iso18626ReShareShim)
	default:
		shim = new(Iso18626DefaultShim)
	}
	return shim
}

type Iso18626DefaultShim struct {
}

func (i *Iso18626DefaultShim) ApplyToOutgoing(message *iso18626.ISO18626Message) ([]byte, error) {
	return xml.Marshal(message)
}

func (i *Iso18626DefaultShim) ApplyToIncoming(bytes []byte, message *iso18626.ISO18626Message) error {
	return xml.Unmarshal(bytes, message)
}

type Iso18626AlmaShim struct {
	Iso18626DefaultShim
}

func (i *Iso18626AlmaShim) ApplyToOutgoing(message *iso18626.ISO18626Message) ([]byte, error) {
	if message != nil {
		if message.SupplyingAgencyMessage != nil {
			suppMsg := message.SupplyingAgencyMessage
			i.fixReasonForMessage(suppMsg)
			status := suppMsg.StatusInfo.Status
			if status == iso18626.TypeStatusLoaned {
				i.stripReShareSuppMsgNote(suppMsg)
				i.appendReturnAddressToSuppMsgNote(suppMsg)
			}
		}
		if message.Request != nil {
			request := message.Request
			i.fixServiceLevel(request)
			i.fixBibItemIds(request)
			i.fixBibRecIds(request)
			i.stripReShareReqNote(request)
			i.appendDeliveryAddressToReqNote(request)
			i.appendReturnAddressToReqNote(request)
		}
	}
	return xml.Marshal(message)
}

func (i *Iso18626AlmaShim) stripReShareSuppMsgNote(suppMsg *iso18626.SupplyingAgencyMessage) {
	suppMsg.MessageInfo.Note = rsNoteRegexp.ReplaceAllString(suppMsg.MessageInfo.Note, "")
}

func (i *Iso18626AlmaShim) stripReShareReqNote(request *iso18626.Request) {
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

func (*Iso18626AlmaShim) fixReasonForMessage(suppMsg *iso18626.SupplyingAgencyMessage) {
	reason := suppMsg.MessageInfo.ReasonForMessage
	if reason == iso18626.TypeReasonForMessageRequestResponse || reason == iso18626.TypeReasonForMessageStatusChange {
		status := suppMsg.StatusInfo.Status
		if status == iso18626.TypeStatusWillSupply {
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
		}
		if status == iso18626.TypeStatusLoaned {
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
		}
		if status == iso18626.TypeStatusLoanCompleted {
			suppMsg.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
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
	if addr.Country != nil {
		sb.WriteString(addr.Country.Text)
		sb.WriteString("\n")
	}
}

type Iso18626ReShareShim struct {
	Iso18626DefaultShim
}

func (i *Iso18626ReShareShim) ApplyToIncoming(bytes []byte, message *iso18626.ISO18626Message) error {
	err := xml.Unmarshal(bytes, message)
	if err != nil {
		return err
	}
	if message != nil && message.SupplyingAgencyMessage != nil {
		i.fixSupplierConditionNote(message.SupplyingAgencyMessage)
	}
	return nil
}

func (i *Iso18626ReShareShim) fixSupplierConditionNote(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage) {
	if strings.Contains(supplyingAgencyMessage.MessageInfo.Note, SUPPLIER_AWAITING_CONDITION) {
		supplyingAgencyMessage.MessageInfo.Note = "Conditions pending \nPlease respond `ACCEPT` or `REJECT`"
	}
}

func (i *Iso18626ReShareShim) ApplyToOutgoing(message *iso18626.ISO18626Message) ([]byte, error) {
	if message != nil && message.RequestingAgencyMessage != nil {
		i.fixRequesterConditionNote(message.RequestingAgencyMessage)
	}
	return xml.Marshal(message)
}
func (i *Iso18626ReShareShim) fixRequesterConditionNote(requestingAgencyMessage *iso18626.RequestingAgencyMessage) {
	if requestingAgencyMessage.Action == iso18626.TypeActionNotification {
		if strings.EqualFold(requestingAgencyMessage.Note, ACCEPT) {
			requestingAgencyMessage.Note = LOAN_CONDITION_AGREE
		} else if strings.EqualFold(requestingAgencyMessage.Note, REJECT) {
			requestingAgencyMessage.Action = iso18626.TypeActionCancel
		}
	}
}
