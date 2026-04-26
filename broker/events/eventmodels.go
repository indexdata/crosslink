package events

import (
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
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

type EventDomain string

const (
	EventDomainPatronRequest  EventDomain = "PATRON_REQUEST"
	EventDomainIllTransaction EventDomain = "ILL_TRANSACTION"
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
	EventNameConfirmSupplierMsg   EventName = "confirm-supplier-msg"
	EventNameInvokeAction         EventName = "invoke-action"
	EventNamePatronRequestMessage EventName = "patron-request-message"
	EventNameLmsRequesterMessage  EventName = "lms-requester-message"
	EventNameLmsSupplierMessage   EventName = "lms-supplier-message"
	EventNameSendNotification     EventName = "send-notification"
)

type Signal string

const (
	SignalTaskBegin     Signal = "task_begin"
	SignalTaskComplete  Signal = "task_complete"
	SignalTaskCreated   Signal = "task_created"
	SignalNoticeCreated Signal = "notice_created"
)

// SignalTarget controls which handler role receives the signal.
type SignalTarget string

const (
	SignalConsumers SignalTarget = "consumers"
	SignalObservers SignalTarget = "observers"
	SignalAll       SignalTarget = "all"
)

// HandlerRole controls which signal target this handler supports.
type HandlerRole string

const (
	HandlerRoleConsumer HandlerRole = "consumer" // claims/locks the event
	HandlerRoleObserver HandlerRole = "observer" // doesn't claim/lock the event
)

type EventData struct {
	CommonEventData
	CustomData map[string]any `json:"customData,omitempty"`
}

type CommonEventData struct {
	IncomingMessage *iso18626.ISO18626Message  `json:"incomingMessage,omitempty"`
	OutgoingMessage *iso18626.ISO18626Message  `json:"outgoingMessage,omitempty"`
	Problem         *Problem                   `json:"problem,omitempty"`
	HttpFailure     *httpclient.HttpError      `json:"httpFailure,omitempty"`
	EventError      *EventError                `json:"eventError,omitempty"`
	Note            string                     `json:"note,omitempty"`
	User            string                     `json:"user,omitempty"`
	Action          *pr_db.PatronRequestAction `json:"action,omitempty"`
	ActionResult    *ActionResult              `json:"actionResult,omitempty"`
	Notification    *pr_db.Notification        `json:"notification,omitempty"`
}

type ActionResult struct {
	Outcome          string  `json:"outcome"`
	ToState          *string `json:"toState,omitempty"`
	ChildActionError *string `json:"childActionError,omitempty"`
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
	Event  string       `json:"event"`
	Signal Signal       `json:"signal"`
	Target SignalTarget `json:"target"`
}

func NewErrorResult(message string, cause string) (EventStatus, *EventResult) {
	return EventStatusError, &EventResult{
		CommonEventData: CommonEventData{
			EventError: &EventError{
				Message: message,
				Cause:   cause,
			},
		},
	}
}

func NewProblemResult(kind string, details string) (EventStatus, *EventResult) {
	return EventStatusProblem, &EventResult{
		CommonEventData: CommonEventData{
			Problem: &Problem{
				Kind:    kind,
				Details: details,
			},
		},
	}
}
