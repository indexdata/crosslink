package prservice

import (
	"slices"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultLoanActionMapping(t *testing.T) {
	borrowerStateActionMapping := map[pr_db.PatronRequestState][]PatronRequestAction{
		BorrowerStateNew:              {{actionName: BorrowerActionValidatePatron, auto: true}, {actionName: BorrowerActionSkipPatronValidation}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateInvalidPatron:    {{actionName: BorrowerActionValidatePatron}, {actionName: BorrowerActionSkipPatronValidation}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateValidated:        {{actionName: BorrowerActionUpdateMetadata, auto: true}, {actionName: BorrowerActionSkipMetadataUpdate}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateMetadataUpdated:  {{actionName: BorrowerActionSendRequest, auto: true}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateNeedsReview:      {{actionName: BorrowerActionSendRequest}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateDuplicate:        {{actionName: BorrowerActionSendRequest}, {actionName: BorrowerActionCloseRequest}},
		BorrowerStateSupplierLocated:  {{actionName: BorrowerActionCancelRequest}},
		BorrowerStateConditionPending: {{actionName: BorrowerActionAcceptCondition}, {actionName: BorrowerActionRejectCondition}},
		BorrowerStateWillSupply:       {{actionName: BorrowerActionCancelRequest}},
		BorrowerStateShipped:          {{actionName: BorrowerActionReceive}},
		BorrowerStateReceived:         {{actionName: BorrowerActionCheckOut}, {actionName: BorrowerActionSendNotification, auto: true}},
		BorrowerStateCheckedOut:       {{actionName: BorrowerActionCheckIn}},
		BorrowerStateCheckedIn:        {{actionName: BorrowerActionShipReturn}},
		BorrowerStateRetryPending:     {{actionName: BorrowerActionAcceptRetry}, {actionName: BorrowerActionRejectRetry}},
		BorrowerStateCancelled:        {{actionName: BorrowerActionSendNotification, auto: true}},
		BorrowerStateUnfilled:         {{actionName: BorrowerActionSendNotification, auto: true}},
		BorrowerStateLocalSupply:      {{actionName: BorrowerActionFillLocally}, {actionName: BorrowerActionCancelLocalSupply}, {actionName: BorrowerActionCannotSupplyLocally}},
	}

	lenderStateActionMapping := map[pr_db.PatronRequestState][]PatronRequestAction{
		LenderStateNew:               {{actionName: LenderActionSendNotification, auto: true}, {actionName: LenderActionValidatePatron, auto: true}},
		LenderStateValidated:         {{actionName: LenderActionWillSupply, auto: true}, {actionName: LenderActionCannotSupply}, {actionName: LenderActionAddCondition}, {actionName: LenderActionAskRetry}},
		LenderStateWillSupply:        {{actionName: LenderActionAddItem}, {actionName: LenderActionRemoveItem}, {actionName: LenderActionAddCondition}, {actionName: LenderActionShip}, {actionName: LenderActionCannotSupply}, {actionName: LenderActionAskRetry}},
		LenderStateConditionPending:  {{actionName: LenderActionAddCondition}, {actionName: LenderActionCannotSupply}},
		LenderStateConditionAccepted: {{actionName: LenderActionAddItem}, {actionName: LenderActionRemoveItem}, {actionName: LenderActionAddCondition}, {actionName: LenderActionShip}, {actionName: LenderActionCannotSupply}},
		LenderStateShippedReturn:     {{actionName: LenderActionMarkReceived}},
		LenderStateCancelRequested:   {{actionName: LenderActionAcceptCancel}, {actionName: LenderActionRejectCancel}},
	}

	stateModel, err := LoadStateModelByName("default")
	assert.Nil(t, err)
	loanActionMapping := NewActionMappingForServiceType(stateModel, proapi.Loan)

	assert.NotNil(t, loanActionMapping)

	mapCompare(t, borrowerStateActionMapping, loanActionMapping.borrowerStateActionMapping)

	mapCompare(t, lenderStateActionMapping, loanActionMapping.lenderStateActionMapping)
}

var actionMappingService = ActionMappingService{}

func mustActionMapping(t *testing.T) *ActionMapping {
	t.Helper()
	mapping, err := actionMappingService.GetActionMapping(iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	})
	assert.NoError(t, err)
	assert.NotNil(t, mapping)
	return mapping
}

func TestResolveActionMappingUsesSelector(t *testing.T) {
	service := ActionMappingService{}

	for _, serviceType := range []iso18626.TypeServiceType{
		iso18626.TypeServiceTypeCopy,
		iso18626.TypeServiceTypeLoan,
		iso18626.TypeServiceTypeCopyOrLoan,
	} {
		t.Run(string(serviceType), func(t *testing.T) {
			name, mapping, err := service.ResolveActionMapping(iso18626.Request{
				ServiceInfo: &iso18626.ServiceInfo{ServiceType: serviceType},
			})
			assert.NoError(t, err)
			assert.Equal(t, "default", name)
			if assert.NotNil(t, mapping) {
				assert.Equal(t, "CrossLink State Model", mapping.StateModelName)
			}
		})
	}
}

func TestResolveActionMappingWithoutServiceInfoUsesLegacyLoanDefault(t *testing.T) {
	name, mapping, err := (&ActionMappingService{}).ResolveActionMapping(iso18626.Request{})

	assert.NoError(t, err)
	assert.Equal(t, "default", name)
	if assert.NotNil(t, mapping) {
		willSupply := pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}
		assert.True(t, mapping.IsActionSupported(willSupply, LenderActionShip))
		assert.False(t, mapping.IsActionSupported(willSupply, LenderActionSupplyDocument))
	}
}

func TestGetActionMappingAppliesServiceType(t *testing.T) {
	service := &ActionMappingService{SMService: &StateModelService{}}
	copyRequest := iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopy},
	}
	copyMapping, err := service.GetActionMapping(copyRequest)
	assert.NoError(t, err)
	loanMapping, err := service.GetActionMapping(iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	})
	assert.NoError(t, err)
	cachedCopyMapping, err := service.GetActionMapping(copyRequest)
	assert.NoError(t, err)
	assert.Same(t, copyMapping, cachedCopyMapping)
	assert.NotSame(t, copyMapping, loanMapping)
	copyOrLoanMapping, err := service.GetActionMapping(iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopyOrLoan},
	})
	assert.NoError(t, err)

	willSupply := pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}
	assert.True(t, copyMapping.IsActionSupported(willSupply, LenderActionSupplyDocument))
	assert.False(t, copyMapping.IsActionSupported(willSupply, LenderActionShip))
	assert.False(t, copyMapping.IsActionSupported(willSupply, LenderActionAddItem))
	assert.False(t, copyMapping.IsActionSupported(willSupply, LenderActionRemoveItem))
	assert.True(t, copyMapping.IsActionSupported(willSupply, LenderActionAddCondition), "an action without appliesTo must apply to Copy")
	assert.True(t, loanMapping.IsActionSupported(willSupply, LenderActionShip))
	assert.True(t, loanMapping.IsActionSupported(willSupply, LenderActionAddItem))
	assert.True(t, loanMapping.IsActionSupported(willSupply, LenderActionRemoveItem))
	assert.False(t, loanMapping.IsActionSupported(willSupply, LenderActionSupplyDocument))
	assert.True(t, copyOrLoanMapping.IsActionSupported(willSupply, LenderActionShip))
	assert.True(t, copyOrLoanMapping.IsActionSupported(willSupply, LenderActionAddItem))
	assert.True(t, copyOrLoanMapping.IsActionSupported(willSupply, LenderActionRemoveItem))
	assert.True(t, copyOrLoanMapping.IsActionSupported(willSupply, LenderActionSupplyDocument))

	localSupply := pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateLocalSupply}
	assert.True(t, copyMapping.IsActionSupported(localSupply, BorrowerActionSupplyDocument))
	assert.False(t, copyMapping.IsActionSupported(localSupply, BorrowerActionFillLocally))
	assert.True(t, loanMapping.IsActionSupported(localSupply, BorrowerActionFillLocally))
	assert.False(t, loanMapping.IsActionSupported(localSupply, BorrowerActionSupplyDocument))
	assert.True(t, copyOrLoanMapping.IsActionSupported(localSupply, BorrowerActionFillLocally))
	assert.True(t, copyOrLoanMapping.IsActionSupported(localSupply, BorrowerActionSupplyDocument))

	_, copyHasShippedState := copyMapping.getStateConfig(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped})
	_, loanHasShippedState := loanMapping.getStateConfig(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped})
	assert.False(t, copyHasShippedState)
	assert.True(t, loanHasShippedState)

	copyActions := copyMapping.GetAllowedActionsForPatronRequest(willSupply, true)
	supplyDocumentIndex := slices.IndexFunc(copyActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(LenderActionSupplyDocument)
	})
	if assert.NotEqual(t, -1, supplyDocumentIndex) {
		assert.NotNil(t, copyActions.Actions[supplyDocumentIndex].Primary)
		assert.True(t, *copyActions.Actions[supplyDocumentIndex].Primary)
		assert.Equal(t, []string{"note", "deliveryUrl"}, copyActions.Actions[supplyDocumentIndex].Parameters)
	}

	copyOrLoanActions := copyOrLoanMapping.GetAllowedActionsForPatronRequest(willSupply, true)
	shipIndex := slices.IndexFunc(copyOrLoanActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(LenderActionShip)
	})
	supplyDocumentIndex = slices.IndexFunc(copyOrLoanActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(LenderActionSupplyDocument)
	})
	if assert.NotEqual(t, -1, shipIndex) {
		assert.NotNil(t, copyOrLoanActions.Actions[shipIndex].Primary)
		assert.True(t, *copyOrLoanActions.Actions[shipIndex].Primary)
	}
	if assert.NotEqual(t, -1, supplyDocumentIndex) {
		assert.Nil(t, copyOrLoanActions.Actions[supplyDocumentIndex].Primary)
	}

	copyLocalActions := copyMapping.GetAllowedActionsForPatronRequest(localSupply, true)
	localSupplyDocumentIndex := slices.IndexFunc(copyLocalActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(BorrowerActionSupplyDocument)
	})
	if assert.NotEqual(t, -1, localSupplyDocumentIndex) {
		assert.NotNil(t, copyLocalActions.Actions[localSupplyDocumentIndex].Primary)
		assert.True(t, *copyLocalActions.Actions[localSupplyDocumentIndex].Primary)
		assert.Equal(t, []string{"note", "deliveryUrl"}, copyLocalActions.Actions[localSupplyDocumentIndex].Parameters)
	}

	copyOrLoanLocalActions := copyOrLoanMapping.GetAllowedActionsForPatronRequest(localSupply, true)
	fillLocallyIndex := slices.IndexFunc(copyOrLoanLocalActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(BorrowerActionFillLocally)
	})
	localSupplyDocumentIndex = slices.IndexFunc(copyOrLoanLocalActions.Actions, func(action proapi.AllowedAction) bool {
		return action.Name == string(BorrowerActionSupplyDocument)
	})
	if assert.NotEqual(t, -1, fillLocallyIndex) {
		assert.NotNil(t, copyOrLoanLocalActions.Actions[fillLocallyIndex].Primary)
		assert.True(t, *copyOrLoanLocalActions.Actions[fillLocallyIndex].Primary)
	}
	if assert.NotEqual(t, -1, localSupplyDocumentIndex) {
		assert.Nil(t, copyOrLoanLocalActions.Actions[localSupplyDocumentIndex].Primary)
	}
}

func TestResolveActionMappingWithoutMatch(t *testing.T) {
	name, mapping, err := (&ActionMappingService{}).ResolveActionMapping(iso18626.Request{
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceType("Unsupported")},
	})

	assert.Empty(t, name)
	assert.Nil(t, mapping)
	assert.EqualError(t, err, `no state model matches service type "Unsupported"`)
}

func TestIsActionAvailable(t *testing.T) {
	mapping := mustActionMapping(t)
	// Borrower
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionValidatePatron))
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionSkipPatronValidation))
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionCloseRequest))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionReceive))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}, TerminateAction))

	// Lender
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, LenderActionShip))
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending}, LenderActionAddCondition))
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionAccepted}, LenderActionAddCondition))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, BorrowerActionRejectCondition))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, TerminateAction))
}

func TestGetActionsForPatronRequest(t *testing.T) {
	mapping := mustActionMapping(t)
	// Borrower
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSkipPatronValidation, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionValidatePatron, BorrowerActionSkipPatronValidation, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidatePatron), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusError), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionValidatePatron, BorrowerActionSkipPatronValidation, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidatePatron), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusProblem), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSkipPatronValidation, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidatePatron), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusSuccess), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCompleted}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCancelled}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSendRequest, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNeedsReview}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSendRequest, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateDuplicate}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionValidatePatron, BorrowerActionSkipPatronValidation, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateInvalidPatron}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSkipMetadataUpdate, BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionCloseRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateMetadataUpdated}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionCancelRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateSupplierLocated}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionAcceptCondition, BorrowerActionRejectCondition}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateConditionPending}))

	// Lender
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddItem, LenderActionRemoveItem, LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip, LenderActionAskRetry}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddCondition, LenderActionCannotSupply}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending}))
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddItem, LenderActionRemoveItem, LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionAccepted}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped}))
}

func TestGetManualCloseState(t *testing.T) {
	mapping := mustActionMapping(t)

	state, ok := mapping.GetManualCloseState(pr_db.PatronRequest{Side: SideBorrowing})
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateManuallyClosed, state)

	state, ok = mapping.GetManualCloseState(pr_db.PatronRequest{Side: SideLending})
	assert.True(t, ok)
	assert.Equal(t, LenderStateManuallyClosed, state)
}

func TestGetManualCloseStateMissing(t *testing.T) {
	tt := true
	mapping := NewActionMappingForServiceType(&proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:     string(BorrowerStateCompleted),
				Side:     proapi.REQUESTER,
				Terminal: &tt,
			},
		},
	}, proapi.Loan)

	_, ok := mapping.GetManualCloseState(pr_db.PatronRequest{Side: SideBorrowing})
	assert.False(t, ok)
}

func TestGetAllowedActionsForPatronRequest1(t *testing.T) {
	mapping := mustActionMapping(t)
	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{
		{Name: string(BorrowerActionSkipPatronValidation), Parameters: []string{}, Available: true},
		{Name: string(BorrowerActionCloseRequest), Parameters: []string{}, Available: true},
	}}, mapping.GetAllowedActionsForPatronRequest(
		pr_db.PatronRequest{
			Side: SideBorrowing, State: BorrowerStateNew}, true))

	tt := true
	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{
		{Name: string(BorrowerActionSkipMetadataUpdate), Parameters: []string{}, Available: false},
		{Name: string(BorrowerActionCloseRequest), Parameters: []string{}, Available: false},
	}},
		mapping.GetAllowedActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}, false))

	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{
		{Name: string(LenderActionAddItem), Parameters: []string{"barcode", "callNumber", "title", "itemId"}, Available: true},
		{Name: string(LenderActionRemoveItem), Parameters: []string{"barcode"}, Available: true},
		{Name: string(LenderActionAddCondition), Parameters: []string{"note", "loanCondition", "cost", "currency"}, Available: true},
		{Name: string(LenderActionShip), Parameters: []string{"note"}, Primary: &tt, Available: true},
		{Name: string(LenderActionCannotSupply), Parameters: []string{"note", "reasonUnfilled"}, Available: true},
		{Name: string(LenderActionAskRetry), Parameters: []string{"note", "reasonRetry", "itemId"}, Available: true},
	}}, mapping.GetAllowedActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, true))

	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{
		{Name: string(BorrowerActionValidatePatron), Parameters: []string{}, Primary: &tt, Available: true},
		{Name: string(BorrowerActionSkipPatronValidation), Parameters: []string{}, Available: true},
		{Name: string(BorrowerActionCloseRequest), Parameters: []string{}, Available: true},
	}}, mapping.GetAllowedActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateInvalidPatron}, true))
}

func TestGetActionTransitionMissingCases(t *testing.T) {
	mapping := mustActionMapping(t)

	// Supported action, but failure transition is not defined in state model	.
	_, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew},
		BorrowerActionValidatePatron,
		ActionOutcomeFailure,
	)
	assert.False(t, ok)

	// Unsupported outcome key should not resolve any transition.
	_, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew},
		BorrowerActionValidatePatron,
		"unknown-outcome",
	)
	assert.False(t, ok)

	// Action not configured for state should not resolve transition.
	_, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated},
		BorrowerActionValidatePatron,
		ActionOutcomeSuccess,
	)
	assert.False(t, ok)
}

func TestInvalidPatronValidateTransitions(t *testing.T) {
	mapping := mustActionMapping(t)

	state, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew},
		BorrowerActionValidatePatron,
		ActionOutcomeReview,
	)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateInvalidPatron, state)

	state, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateInvalidPatron},
		BorrowerActionValidatePatron,
		ActionOutcomeReview,
	)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateInvalidPatron, state)

	state, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateInvalidPatron},
		BorrowerActionValidatePatron,
		ActionOutcomeSuccess,
	)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateValidated, state)
}

func TestStateModelTransitionActions(t *testing.T) {
	mapping := mustActionMapping(t)
	pr := pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateInvalidPatron}

	assert.True(t, mapping.IsTransitionAction(pr, BorrowerActionSkipPatronValidation))
	state, ok := mapping.GetActionTransition(pr, BorrowerActionSkipPatronValidation, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateValidated, state)

	assert.True(t, mapping.IsTransitionAction(pr, BorrowerActionCloseRequest))
	state, ok = mapping.GetActionTransition(pr, BorrowerActionCloseRequest, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateManuallyClosed, state)

	pr = pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}
	assert.True(t, mapping.IsTransitionAction(pr, BorrowerActionSkipMetadataUpdate))
	state, ok = mapping.GetActionTransition(pr, BorrowerActionSkipMetadataUpdate, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateMetadataUpdated, state)

	assert.True(t, mapping.IsTransitionAction(pr, BorrowerActionCloseRequest))
	state, ok = mapping.GetActionTransition(pr, BorrowerActionCloseRequest, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateManuallyClosed, state)

	pr = pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateMetadataUpdated}
	assert.True(t, mapping.IsTransitionAction(pr, BorrowerActionCloseRequest))
	state, ok = mapping.GetActionTransition(pr, BorrowerActionCloseRequest, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateManuallyClosed, state)

	assert.True(t, mapping.IsTransitionAction(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateRetryPending},
		BorrowerActionRejectRetry,
	))
	assert.True(t, mapping.IsTransitionAction(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateDuplicate},
		BorrowerActionCloseRequest,
	))

	duplicatePr := pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateDuplicate}
	assert.False(t, mapping.IsTransitionAction(duplicatePr, BorrowerActionSendRequest))
	state, ok = mapping.GetActionTransition(duplicatePr, BorrowerActionSendRequest, ActionOutcomeSuccess)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateSent, state)
	state, ok = mapping.GetActionTransition(duplicatePr, BorrowerActionSendRequest, ActionOutcomeDuplicate)
	assert.True(t, ok)
	assert.Equal(t, BorrowerStateDuplicate, state)
}

func TestGetActionTransitionConditionPendingSelfTransition(t *testing.T) {
	mapping := mustActionMapping(t)

	transition, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending},
		LenderActionAddCondition,
		ActionOutcomeSuccess,
	)
	assert.True(t, ok)
	assert.Equal(t, LenderStateConditionPending, transition)
}

func TestGetActionTransitionWillSupplyFailureSelfTransition(t *testing.T) {
	mapping := mustActionMapping(t)

	transition, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideLending, State: LenderStateValidated},
		LenderActionWillSupply,
		ActionOutcomeFailure,
	)
	assert.True(t, ok)
	assert.Equal(t, LenderStateValidated, transition)
}

func TestGetEventTransitionRetryConditionalFromBorrowerWillSupply(t *testing.T) {
	mapping := mustActionMapping(t)

	transition, stateChanged, eventDefined := mapping.GetEventTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateWillSupply},
		string(SupplierRetryConditional),
	)
	assert.True(t, eventDefined)
	assert.True(t, stateChanged)
	assert.Equal(t, BorrowerStateRetryPending, transition)
}

func TestGetClosingAction(t *testing.T) {
	mapping := mustActionMapping(t)

	action := mapping.GetClosingAction(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateDuplicate})
	assert.NotNil(t, action)
	assert.Equal(t, BorrowerActionCloseRequest, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew})
	assert.NotNil(t, action)
	assert.Equal(t, BorrowerActionCloseRequest, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated})
	assert.NotNil(t, action)
	assert.Equal(t, BorrowerActionCloseRequest, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateMetadataUpdated})
	assert.NotNil(t, action)
	assert.Equal(t, BorrowerActionCloseRequest, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNeedsReview})
	assert.NotNil(t, action)
	assert.Equal(t, BorrowerActionCloseRequest, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideLending, State: LenderStateValidated})
	assert.NotNil(t, action)
	assert.Equal(t, LenderActionCannotSupply, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply})
	assert.NotNil(t, action)
	assert.Equal(t, LenderActionCannotSupply, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending})
	assert.NotNil(t, action)
	assert.Equal(t, LenderActionCannotSupply, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionAccepted})
	assert.NotNil(t, action)
	assert.Equal(t, LenderActionCannotSupply, *action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped})
	assert.Nil(t, action)

	action = mapping.GetClosingAction(pr_db.PatronRequest{State: LenderStateWillSupply})
	assert.Nil(t, action)
}

func listCompare(t *testing.T, list1 []pr_db.PatronRequestAction, list2 []pr_db.PatronRequestAction) {
	assert.Equal(t, len(list1), len(list2), "list1=%v, list2=%v", list1, list2)
	for i := range list1 {
		assert.True(t, slices.Contains(list2, list1[i]), "list1=%v, list2=%v", list1, list2)
	}
}

func mapCompare(t *testing.T, map1 map[pr_db.PatronRequestState][]PatronRequestAction, map2 map[pr_db.PatronRequestState][]PatronRequestAction) {
	for stateName := range map1 {
		listOne := map1[stateName]
		listTwo := map2[stateName]
		assert.Equal(t, len(listOne), len(listTwo), "State %s has different number of actions in the two maps", stateName)
		for i := range listOne {
			assert.Equal(t, listOne[i].actionName, listTwo[i].actionName)
			assert.Equal(t, listOne[i].auto, listTwo[i].auto, "State %s has different auto action configuration for action %s", stateName, listOne[i].actionName)
		}
	}
}
