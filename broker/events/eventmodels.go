package events

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

type Signal string

const (
	SignalTaskBegin     Signal = "task_begin"
	SignalTaskComplete  Signal = "task_complete"
	SignalTaskCreated   Signal = "task_created"
	SignalNoticeCreated Signal = "notice_created"
)

type EventData struct {
	Timestamp       pgtype.Timestamp
	ISO18626Message *iso18626.ISO18626Message `json:"iso18626Message,omitempty"`
}

type EventResult struct {
	Data map[string]any
}

type NotifyData struct {
	Event  string `json:"event"`
	Signal Signal `json:"signal"`
}
