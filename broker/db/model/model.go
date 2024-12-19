package model

import (
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventState string

const (
	EventStateNew        EventState = "NEW"
	EventStateProcessing EventState = "PROCESSING"
	EventStateSuccess    EventState = "SUCCESS"
	EventStateProblem    EventState = "PROBLEM"
	EventStateError      EventState = "ERROR"
)

type EventType string

const (
	EventTypeTask   EventType = "TASK"
	EventTypeNotice EventType = "NOTICE"
)

type EventName string

const (
	EventNameRequestTerminated    EventName = "request-terminated"
	EventNameFindSupplier         EventName = "find-supplier"
	EventNameSupplierFound        EventName = "supplier-found"
	EventNameFindSuppliersFailed  EventName = "find-suppliers-failed"
	EventNameSuppliersExhausted   EventName = "suppliers-exhausted"
	EventNameSupplierMsgReceived  EventName = "supplier-msg-received"
	EventNameNotifyRequester      EventName = "notify-requester"
	EventNameRequesterMsgReceived EventName = "requester-msg-received"
	EventNameNotifySupplier       EventName = "notify-supplier"
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

type EventData struct {
	Timestamp       pgtype.Timestamp
	ISO18626Message *iso18626.ISO18626Message `json:"iso18626Message,omitempty"`
}

type EventResult struct {
	Data map[string]any
}
