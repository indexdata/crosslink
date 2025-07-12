package events

import (
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/iso18626"
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
	EventNameConfirmRequesterMsg  EventName = "confirm-requester-msg"
)

type Signal string

const (
	SignalTaskBegin     Signal = "task_begin"
	SignalTaskComplete  Signal = "task_complete"
	SignalTaskCreated   Signal = "task_created"
	SignalNoticeCreated Signal = "notice_created"
)

type EventData struct {
	CommonEventData
	CustomData map[string]any
}

type CommonEventData struct {
	IncomingMessage *iso18626.ISO18626Message `json:"incomingMessage,omitempty"`
	OutgoingMessage *iso18626.ISO18626Message `json:"outgoingMessage,omitempty"`
	Problem         *Problem                  `json:"problem,omitempty"`
	HttpFailure     *httpclient.HttpError     `json:"httpFailure,omitempty"`
	EventError      *EventError               `json:"eventError,omitempty"`
	Note            string                    `json:"note,omitempty"`
}

type EventError struct {
	Message string
	Cause   string
}

type Problem struct {
	Kind    string
	Details string
}

type EventResult struct {
	CommonEventData
	CustomData map[string]any `json:"customData,omitempty"`
}

type NotifyData struct {
	Event  string `json:"event"`
	Signal Signal `json:"signal"`
}
