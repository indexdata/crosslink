package prservice

import (
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/stretchr/testify/assert"
)

func TestActionTriggerAvailability(t *testing.T) {
	for _, side := range []proapi.ModelStateSide{proapi.REQUESTER, proapi.SUPPLIER} {
		for _, tc := range []struct {
			name                 string
			trigger              *proapi.ModelActionTrigger
			result               events.EventStatus
			available, automatic bool
		}{
			{name: "omitted defaults to manual", available: true},
			{name: "manual", trigger: ptr(proapi.Manual), available: true},
			{name: "failed manual remains available", trigger: ptr(proapi.Manual), result: events.EventStatusError, available: true},
			{name: "automatic entry", trigger: ptr(proapi.Auto), automatic: true},
			{name: "failed automatic retry", trigger: ptr(proapi.Auto), result: events.EventStatusError, automatic: true, available: true},
			{name: "problem automatic retry", trigger: ptr(proapi.Auto), result: events.EventStatusProblem, automatic: true, available: true},
			{name: "background", trigger: ptr(proapi.Background)},
			{name: "failed background stays hidden", trigger: ptr(proapi.Background), result: events.EventStatusError},
			{name: "problem background stays hidden", trigger: ptr(proapi.Background), result: events.EventStatusProblem},
		} {
			t.Run(string(side)+"/"+tc.name, func(t *testing.T) {
				action := proapi.ModelAction{Name: "validate-patron", Trigger: tc.trigger}
				mapping := NewActionMappingForServiceType(&proapi.StateModel{States: []proapi.ModelState{{
					Name: "NEW", Side: side, Actions: &[]proapi.ModelAction{action},
				}}}, proapi.Loan)
				pr := pr_db.PatronRequest{State: "NEW", Side: SideBorrowing,
					LastAction: getDbText(action.Name), LastActionResult: getDbText(string(tc.result))}
				if side == proapi.SUPPLIER {
					pr.Side = SideLending
				}
				assert.True(t, mapping.IsActionSupported(pr, pr_db.PatronRequestAction(action.Name)))
				assert.Equal(t, tc.available, mapping.IsActionAvailable(pr, pr_db.PatronRequestAction(action.Name)))
				if tc.available {
					assert.Len(t, mapping.GetAllowedActionsForPatronRequest(pr, true).Actions, 1)
				} else {
					assert.Empty(t, mapping.GetAllowedActionsForPatronRequest(pr, true).Actions)
				}
				if tc.automatic {
					assert.Len(t, mapping.GetAutoActionsForState(pr), 1)
				} else {
					assert.Empty(t, mapping.GetAutoActionsForState(pr), "only auto triggers may run on state entry")
				}
			})
		}
	}
}

func TestDefaultOverdueIsBackgroundOnly(t *testing.T) {
	mapping := mustActionMapping(t)
	for _, state := range []pr_db.PatronRequestState{LenderStateReceived, LenderStateRenewed, LenderStateRenewalRejected} {
		for _, result := range []events.EventStatus{events.EventStatusSuccess, events.EventStatusProblem, events.EventStatusError} {
			pr := pr_db.PatronRequest{State: state, Side: SideLending,
				LastAction: getDbText(string(LenderActionOverdue)), LastActionResult: getDbText(string(result))}
			assert.True(t, mapping.IsActionSupported(pr, LenderActionOverdue))
			assert.False(t, mapping.IsActionAvailable(pr, LenderActionOverdue), "%s/%s", state, result)
		}
	}
}
