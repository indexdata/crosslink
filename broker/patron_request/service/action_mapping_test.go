package prservice

import (
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/assert"
	"testing"
)

var actionMappingService = ActionMappingService{}

func TestIsActionAvailable(t *testing.T) {
	// Borrower
	assert.True(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionValidate))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionReceive))

	// Lender
	assert.True(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, LenderActionShip))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, BorrowerActionRejectCondition))
}

func TestGetActionsForPatronRequest(t *testing.T) {
	// Borrower
	assert.Equal(t, []pr_db.PatronRequestAction{BorrowerActionValidate}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}))
	assert.Equal(t, []pr_db.PatronRequestAction{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCompleted}))

	// Lender
	assert.Equal(t, []pr_db.PatronRequestAction{LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
	assert.Equal(t, []pr_db.PatronRequestAction{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped}))
}
