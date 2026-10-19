package psservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

type printedBus struct {
	events.EventBus
	ids    []string
	failID string
}

func (b *printedBus) CreateTask(id string, _ events.EventName, _ events.EventData, _ events.EventDomain, _ *string, _ events.SignalTarget) (string, error) {
	b.ids = append(b.ids, id)
	if id == b.failID {
		return "", errors.New("queue unavailable")
	}
	return "task", nil
}

func TestQueuePullslipPrintedEligibilityAndPartialFailure(t *testing.T) {
	requests := []pr_db.PatronRequest{
		{ID: "first", Side: prservice.SideLending, State: prservice.LenderStateWillSupply},
		{ID: "reprint", Side: prservice.SideLending, State: prservice.LenderStateSearching},
		{ID: "shipped", Side: prservice.SideLending, State: prservice.LenderStateShipped},
		{ID: "pending", Side: prservice.SideLending, State: prservice.LenderStateConditionPending},
		{ID: "borrower", Side: prservice.SideBorrowing, State: prservice.BorrowerStateWillSupply},
		{ID: "copy", Side: prservice.SideLending, State: prservice.LenderStateWillSupply, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopy}}},
	}
	for _, failID := range []string{"", "first"} {
		t.Run("failed="+failID, func(t *testing.T) {
			bus := &printedBus{failID: failID}
			err := QueuePullslipPrinted(bus, requests, nil, nil)
			if failID == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, "queue pullslip-printed for first")
			}
			assert.Equal(t, []string{"first", "reprint"}, bus.ids)
		})
	}
}

func TestQueuePullslipPrintedInvalidModelDoesNotStopOtherRequests(t *testing.T) {
	bus := &printedBus{}
	err := QueuePullslipPrinted(bus, []pr_db.PatronRequest{
		{ID: "invalid", Side: prservice.SideLending, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: "unknown"}}},
		{ID: "valid", Side: prservice.SideLending, State: prservice.LenderStateWillSupply},
	}, nil, nil)
	assert.ErrorContains(t, err, "request invalid")
	assert.Equal(t, []string{"valid"}, bus.ids)
}
