package shim

import (
	"encoding/xml"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
)

const VendorAlma = "Alma"
const DELIVERY_ADDRESS_BEGIN = "#SHIP_TO#"
const DELIVERY_ADDRESS_END = "#ST_END#"
const RETURN_ADDRESS_BEGIN = "#RETURN_TO#"
const RETURN_ADDRESS_END = "#RT_END#"

type Iso18626Shim interface {
	ApplyToOutgoing(message *iso18626.ISO18626Message) ([]byte, error)
	ApplyToIncoming(bytes []byte, message *iso18626.ISO18626Message) error
}

// factory method
func GetShim(vendor string) Iso18626Shim {
	var shim Iso18626Shim
	switch vendor {
	case VendorAlma:
		shim = new(Iso18626AlmaShim)
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
	if message != nil && message.SupplyingAgencyMessage != nil {
		suppMsg := message.SupplyingAgencyMessage
		i.fixReasonForMessage(suppMsg)
		status := suppMsg.StatusInfo.Status
		if status == iso18626.TypeStatusLoaned {
			i.appendReturnAddressToNote(suppMsg)
		}
	}
	if message != nil && message.Request != nil {
		request := message.Request
		i.appendDeliveryAddressToNote(request)
	}
	return xml.Marshal(message)
}

func (i *Iso18626AlmaShim) appendReturnAddressToNote(suppMsg *iso18626.SupplyingAgencyMessage) {
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
		sb.WriteString(RETURN_ADDRESS_BEGIN)
		sb.WriteString("\n")
		if len(suppMsg.ReturnInfo.Name) > 0 {
			sb.WriteString(suppMsg.ReturnInfo.Name)
			sb.WriteString("\n")
		}
		MarshalAddress(&sb, addr)
		sb.WriteString(RETURN_ADDRESS_END)
		sb.WriteString("\n")
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

func (i *Iso18626AlmaShim) appendDeliveryAddressToNote(request *iso18626.Request) {
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
					}
					sb.WriteString(DELIVERY_ADDRESS_BEGIN)
					sb.WriteString("\n")
					requester := request.RequestingAgencyInfo
					if requester != nil && requester.Name != "" {
						sb.WriteString(requester.Name)
						sb.WriteString("\n")
					}
					MarshalAddress(&sb, addr)
					sb.WriteString(DELIVERY_ADDRESS_END)
					sb.WriteString("\n")
					if request.ServiceInfo == nil {
						request.ServiceInfo = new(iso18626.ServiceInfo)
					}
					// put in the note
					request.ServiceInfo.Note = sb.String()
					break
				}
			}
		}
	}
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
