package prservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestClassifyIllSend(t *testing.T) {
	okConfirmation := iso18626.NewISO18626Message()
	okConfirmation.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{MessageStatus: iso18626.TypeMessageStatusOK},
	}
	problemConfirmation := iso18626.NewISO18626Message()
	problemConfirmation.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{MessageStatus: iso18626.TypeMessageStatusERROR},
	}
	emptyStatusConfirmation := iso18626.NewISO18626Message()
	emptyStatusConfirmation.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{}
	requestConfirmation := iso18626.NewISO18626Message()
	requestConfirmation.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{MessageStatus: iso18626.TypeMessageStatusOK},
	}
	requestingConfirmation := iso18626.NewISO18626Message()
	requestingConfirmation.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{MessageStatus: iso18626.TypeMessageStatusOK},
	}
	outgoingSupplier := iso18626.NewISO18626Message()
	outgoingSupplier.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	outgoingRequest := iso18626.NewISO18626Message()
	outgoingRequest.Request = &iso18626.Request{}
	outgoingRequester := iso18626.NewISO18626Message()
	outgoingRequester.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}

	tests := []struct {
		name          string
		outgoing      *iso18626.ISO18626Message
		incoming      *iso18626.ISO18626Message
		failure       *httpclient.HttpError
		parseErr      error
		expected      events.EventStatus
		expectedCause string
	}{
		{name: "no outgoing message", expected: events.EventStatusError, expectedCause: "missing outgoing ILL message"},
		{name: "supplier confirmation", outgoing: outgoingSupplier, incoming: okConfirmation, expected: events.EventStatusSuccess},
		{name: "request confirmation", outgoing: outgoingRequest, incoming: requestConfirmation, expected: events.EventStatusSuccess},
		{name: "requester confirmation", outgoing: outgoingRequester, incoming: requestingConfirmation, expected: events.EventStatusSuccess},
		{name: "remote problem", outgoing: outgoingSupplier, incoming: problemConfirmation, expected: events.EventStatusProblem},
		{name: "invalid confirmation status", outgoing: outgoingSupplier, incoming: emptyStatusConfirmation, expected: events.EventStatusError, expectedCause: "invalid confirmation message status"},
		{name: "missing response", outgoing: outgoingSupplier, expected: events.EventStatusError, expectedCause: "missing response"},
		{name: "wrong confirmation", outgoing: outgoingRequester, incoming: okConfirmation, expected: events.EventStatusError, expectedCause: "expected requestingAgencyMessageConfirmation, received supplyingAgencyMessageConfirmation"},
		{name: "malformed response", outgoing: outgoingSupplier, incoming: iso18626.NewISO18626Message(), parseErr: errors.New("unexpected EOF"), expected: events.EventStatusError, expectedCause: "unexpected EOF"},
		{name: "HTTP failure", outgoing: outgoingSupplier, incoming: okConfirmation, failure: &httpclient.HttpError{StatusCode: 500}, expected: events.EventStatusError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &events.EventResult{CommonEventData: events.CommonEventData{
				OutgoingMessage: tt.outgoing,
				IncomingMessage: tt.incoming,
				HttpFailure:     tt.failure,
			}}
			assert.Equal(t, tt.expected, classifyIllSend(result, tt.parseErr))
			if tt.expectedCause == "" {
				assert.Nil(t, result.EventError)
			} else if assert.NotNil(t, result.EventError) {
				assert.Contains(t, result.EventError.Cause, tt.expectedCause)
			}
		})
	}
}

func TestCreateIllMessageNotice(t *testing.T) {
	eventBus := new(MockEventBus)
	outgoing := iso18626.NewISO18626Message()
	outgoing.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
	incoming := iso18626.NewISO18626Message()
	incoming.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{MessageStatus: iso18626.TypeMessageStatusOK},
	}
	parentID := "action-1"
	sender := PatronRequestMessageSender{eventBus: eventBus}
	result := &events.EventResult{CommonEventData: events.CommonEventData{
		OutgoingMessage: outgoing,
		IncomingMessage: incoming,
	}}

	sender.createIllMessageNotice(appCtx, "pr-1", parentID, events.EventNameIllRequesterMessage, result, events.EventStatusSuccess)

	if assert.Len(t, eventBus.createdNoticeData, 1) {
		assert.Equal(t, events.EventNameIllRequesterMessage, eventBus.createdNoticeNames[0])
		assert.Equal(t, events.EventStatusSuccess, eventBus.createdNoticeStatus[0])
		assert.Equal(t, parentID, eventBus.createdNoticeParent[0])
		assert.Same(t, outgoing, eventBus.createdNoticeData[0].OutgoingMessage)
		assert.Same(t, incoming, eventBus.createdNoticeData[0].IncomingMessage)
	}
}

func TestResponseCaptureWriterRetainsParseError(t *testing.T) {
	writer := NewResponseCaptureWriter()

	n, err := writer.Write([]byte("<invalid"))

	assert.Equal(t, len("<invalid"), n)
	assert.Error(t, err)
	assert.Same(t, err, writer.ParseError)
	assert.Equal(t, []byte("<invalid"), writer.Body)
}

func TestActionResultFromIllSendMapsRegularErrorsAndPreservesSendStatus(t *testing.T) {
	attempted := &events.EventResult{CommonEventData: events.CommonEventData{
		OutgoingMessage: iso18626.NewISO18626Message(),
		EventError:      &events.EventError{Message: "invalid ILL response"},
	}}
	result := actionResultFromIllSend(appCtx, events.EventStatusError, attempted, nil, pr_db.PatronRequest{})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Nil(t, result.result.EventError)
	assert.Equal(t, ActionOutcomeFailure, result.result.ActionResult.Outcome)

	result = actionResultFromIllSend(appCtx, events.EventStatusProblem, attempted, nil, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusProblem, result.status)

	preflightErr := errors.New("invalid requester symbol")
	result = actionResultFromIllSend(appCtx, events.EventStatusError, nil, preflightErr, pr_db.PatronRequest{})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, preflightErr.Error(), result.result.EventError.Message)
	assert.Equal(t, ActionOutcomeFailure, result.result.ActionResult.Outcome)
}
