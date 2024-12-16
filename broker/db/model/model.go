package model

import (
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

var EventStateNew = "NEW"
var EventStateProcessing = "PROCESSING"
var EventStateSuccess = "SUCCESS"
var EventStateProblem = "PROBLEM"
var EventStateError = "ERROR"

var EventTypeRequestReceived = "request-received"
var EventTypeRequestTerminated = "request-terminated"
var EventTypeFindSupplier = "find-supplier"
var EventTypeSupplierFound = "supplier-found"
var EventTypeFindSuppliersFailed = "find-suppliers-failed"
var EventTypeSuppliersExhausted = "suppliers-exhausted"
var EventTypeSupplierMsgReceived = "supplier-msg-received"
var EventTypeNotifyRequester = "notify-requester"
var EventTypeRequesterMsgReceived = "requester-msg-received"
var EventTypeNotifySupplier = "notify-supplier"

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

type Event struct {
	Timestamp       pgtype.Timestamp
	ISO18626Message *iso18626.ISO18626Message `json:"iso18626Message,omitempty"`
}
