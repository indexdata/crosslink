package shim

import (
	"encoding/xml"

	"github.com/indexdata/crosslink/iso18626"
)

const VendorAlma = "Alma"

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
		if message.SupplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusWillSupply {
			message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
		}
		if message.SupplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoaned {
			message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
		}
		if message.SupplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoanCompleted {
			message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
		}
	}
	return xml.Marshal(message)
}
