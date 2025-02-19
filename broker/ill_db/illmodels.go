package ill_db

import (
	"github.com/indexdata/crosslink/iso18626"
)

type IllTransactionData struct {
	BibliographicInfo     iso18626.BibliographicInfo       `json:"bibliographicInfo"`
	PublicationInfo       *iso18626.PublicationInfo        `json:"publicationInfo,omitempty"`
	ServiceInfo           *iso18626.ServiceInfo            `json:"serviceInfo,omitempty"`
	SupplierInfo          []iso18626.SupplierInfo          `json:"supplierInfo,omitempty"`
	RequestedDeliveryInfo []iso18626.RequestedDeliveryInfo `json:"requestedDeliveryInfo,omitempty"`
	RequestingAgencyInfo  *iso18626.RequestingAgencyInfo   `json:"requestingAgencyInfo,omitempty"`
	PatronInfo            *iso18626.PatronInfo             `json:"patronInfo,omitempty"`
	BillingInfo           *iso18626.BillingInfo            `json:"billingInfo,omitempty"`
	DeliveryInfo          *iso18626.DeliveryInfo           `json:"deliveryInfo,omitempty"`
	ReturnInfo            *iso18626.ReturnInfo             `json:"returnInfo,omitempty"`
}

type RefreshPolicy string

const (
	RefreshPolicyNever       RefreshPolicy = "never"
	RefreshPolicyTransaction RefreshPolicy = "transaction"
)
