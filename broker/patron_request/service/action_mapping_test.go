package prservice

import (
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/assert"
	"testing"
)

var actionMappingService = ActionMappingService{}

func TestIsActionAvailable(t *testing.T) {
	// Borrower
	assert.True(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, ActionValidate))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, ActionReceive))

	// Lender
	assert.True(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, ActionShip))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, ActionRejectCondition))
}

func TestGetActionsForPatronRequest(t *testing.T) {
	// Borrower
	assert.Equal(t, []string{ActionValidate}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}))
	assert.Equal(t, []string{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCompleted}))

	// Lender
	assert.Equal(t, []string{ActionAddCondition, ActionCannotSupply, ActionShip}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
	assert.Equal(t, []string{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped}))
}
