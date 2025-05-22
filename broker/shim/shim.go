package shim

import (
	"encoding/xml"
	"strings"

	"github.com/indexdata/crosslink/iso18626"
)

const VendorAlma = "Alma"
const ADDRESS_BEGIN = "#REQUESTED DELIVERY ADDRESS BEGIN#"
const ADDRESS_END = "#REQUESTED DELIVERY ADDRESS END#"

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
		reason := message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage
		if reason == iso18626.TypeReasonForMessageRequestResponse || reason == iso18626.TypeReasonForMessageStatusChange {
			status := message.SupplyingAgencyMessage.StatusInfo.Status
			if status == iso18626.TypeStatusWillSupply {
				message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
			}
			if status == iso18626.TypeStatusLoaned {
				message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
			}
			if status == iso18626.TypeStatusLoanCompleted {
				message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
			}
		}
	}
	if message != nil && message.Request != nil {
		request := message.Request
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
								sb.WriteString("\n")
							}
						}
						sb.WriteString(ADDRESS_BEGIN)
						sb.WriteString("\n")
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
						sb.WriteString(ADDRESS_END)
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
	return xml.Marshal(message)
}
