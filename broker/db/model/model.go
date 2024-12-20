package model

import (
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventStatus string

const (
	EventStatusNew        EventStatus = "NEW"
	EventStatusProcessing EventStatus = "PROCESSING"
	EventStatusSuccess    EventStatus = "SUCCESS"
	EventStatusProblem    EventStatus = "PROBLEM"
	EventStatusError      EventStatus = "ERROR"
)

type EventType string

const (
	EventTypeTask   EventType = "TASK"
	EventTypeNotice EventType = "NOTICE"
)

type EventName string

const (
	EventNameRequestTerminated    EventName = "request-terminated"
	EventNameRequestReceived      EventName = "request-received"
	EventNameLocateSuppliers      EventName = "locate-suppliers"
	EventNameSelectSupplier       EventName = "select-supplier"
	EventNameSupplierMsgReceived  EventName = "supplier-msg-received"
	EventNameMessageRequester     EventName = "message-requester"
	EventNameRequesterMsgReceived EventName = "requester-msg-received"
	EventNameMessageSupplier      EventName = "message-supplier"
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
