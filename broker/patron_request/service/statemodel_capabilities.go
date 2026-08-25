package prservice

import (
	"slices"
	"sort"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

type MessageEvent string

const (
	ActionOutcomeSuccess = "success"
	ActionOutcomeFailure = "failure"
	ActionOutcomeReview  = "review"
)

const (
	SideBorrowing pr_db.PatronRequestSide = "borrowing"
	SideLending   pr_db.PatronRequestSide = "lending"
)

const (
	BorrowerStateNew              pr_db.PatronRequestState = "NEW"
	BorrowerStateInvalidPatron    pr_db.PatronRequestState = "INVALID_PATRON"
	BorrowerStateValidated        pr_db.PatronRequestState = "VALIDATED"
	BorrowerStateMetadataUpdated  pr_db.PatronRequestState = "METADATA_UPDATED"
	BorrowerStateNeedsReview      pr_db.PatronRequestState = "NEEDS_REVIEW"
	BorrowerStateReadyToSend      pr_db.PatronRequestState = "READY_TO_SEND"
	BorrowerStateLocalSupply      pr_db.PatronRequestState = "LOCAL_SUPPLY"
	BorrowerStateSent             pr_db.PatronRequestState = "SENT"
	BorrowerStateSupplierLocated  pr_db.PatronRequestState = "SUPPLIER_LOCATED"
	BorrowerStateConditionPending pr_db.PatronRequestState = "CONDITION_PENDING"
	BorrowerStateWillSupply       pr_db.PatronRequestState = "WILL_SUPPLY"
	BorrowerStateShipped          pr_db.PatronRequestState = "SHIPPED"
	BorrowerStateReceived         pr_db.PatronRequestState = "RECEIVED"
	BorrowerStateCheckedOut       pr_db.PatronRequestState = "CHECKED_OUT"
	BorrowerStateCheckedIn        pr_db.PatronRequestState = "CHECKED_IN"
	BorrowerStateShippedReturned  pr_db.PatronRequestState = "SHIPPED_RETURNED"
	BorrowerStateCancelPending    pr_db.PatronRequestState = "CANCEL_PENDING"
	BorrowerStateCompleted        pr_db.PatronRequestState = "COMPLETED"
	BorrowerStateCancelled        pr_db.PatronRequestState = "CANCELLED"
	BorrowerStateUnfilled         pr_db.PatronRequestState = "UNFILLED"
	BorrowerStateRetryPending     pr_db.PatronRequestState = "RETRY_PENDING"
	BorrowerStateRetryAccepted    pr_db.PatronRequestState = "RETRY_ACCEPTED"
	BorrowerStateRetryRejected    pr_db.PatronRequestState = "RETRY_REJECTED"
	BorrowerStateDuplicate        pr_db.PatronRequestState = "DUPLICATE"
	BorrowerStateManuallyClosed   pr_db.PatronRequestState = "MANUALLY_CLOSED"
	BorrowerStateClosedDuplicate  pr_db.PatronRequestState = "CLOSED_DUPLICATE"
	LenderStateNew                pr_db.PatronRequestState = "NEW"
	LenderStateValidated          pr_db.PatronRequestState = "VALIDATED"
	LenderStateItemPending        pr_db.PatronRequestState = "ITEM_PENDING"
	LenderStateWillSupplyPending  pr_db.PatronRequestState = "WILL_SUPPLY_PENDING"
	LenderStateWillSupply         pr_db.PatronRequestState = "WILL_SUPPLY"
	LenderStateConditionPending   pr_db.PatronRequestState = "CONDITION_PENDING"
	LenderStateConditionAccepted  pr_db.PatronRequestState = "CONDITION_ACCEPTED"
	LenderStateShipped            pr_db.PatronRequestState = "SHIPPED"
	LenderStateReceived           pr_db.PatronRequestState = "RECEIVED"
	LenderStateShippedReturn      pr_db.PatronRequestState = "SHIPPED_RETURN"
	LenderStateCancelRequested    pr_db.PatronRequestState = "CANCEL_REQUESTED"
	LenderStateCompleted          pr_db.PatronRequestState = "COMPLETED"
	LenderStateCancelled          pr_db.PatronRequestState = "CANCELLED"
	LenderStateUnfilled           pr_db.PatronRequestState = "UNFILLED"
	LenderStateCompletedWithRetry pr_db.PatronRequestState = "COMPLETED_WITH_RETRY"
	LenderStateManuallyClosed     pr_db.PatronRequestState = "MANUALLY_CLOSED"
)

const (
	BorrowerActionValidatePatron       pr_db.PatronRequestAction = "validate-patron"
	BorrowerActionUpdateMetadata       pr_db.PatronRequestAction = "update-metadata"
	BorrowerActionCheckDuplicate       pr_db.PatronRequestAction = "check-duplicate"
	BorrowerActionSendRequest          pr_db.PatronRequestAction = "send-request"
	BorrowerActionCancelRequest        pr_db.PatronRequestAction = "cancel-request"
	BorrowerActionAcceptCondition      pr_db.PatronRequestAction = "accept-condition"
	BorrowerActionRejectCondition      pr_db.PatronRequestAction = "reject-condition"
	BorrowerActionReceive              pr_db.PatronRequestAction = "receive"
	BorrowerActionCheckOut             pr_db.PatronRequestAction = "check-out"
	BorrowerActionCheckIn              pr_db.PatronRequestAction = "check-in"
	BorrowerActionShipReturn           pr_db.PatronRequestAction = "ship-return"
	BorrowerActionAcceptRetry          pr_db.PatronRequestAction = "accept-retry"
	BorrowerActionRejectRetry          pr_db.PatronRequestAction = "reject-retry"
	BorrowerActionSendNotification     pr_db.PatronRequestAction = "send-notification"
	BorrowerActionFillLocally          pr_db.PatronRequestAction = "fill-locally"
	BorrowerActionSupplyDocument       pr_db.PatronRequestAction = "supply-document"
	BorrowerActionCancelLocalSupply    pr_db.PatronRequestAction = "cancel-local-supply"
	BorrowerActionCannotSupplyLocally  pr_db.PatronRequestAction = "cannot-supply-locally"
	BorrowerActionSkipPatronValidation pr_db.PatronRequestAction = "skip-patron-validation"
	BorrowerActionSkipMetadataUpdate   pr_db.PatronRequestAction = "skip-metadata-update"
	BorrowerActionCloseRequest         pr_db.PatronRequestAction = "close-request"
	LenderActionValidatePatron         pr_db.PatronRequestAction = "validate-patron"
	LenderActionRequestItem            pr_db.PatronRequestAction = "request-item"
	LenderActionWillSupply             pr_db.PatronRequestAction = "will-supply"
	LenderActionRejectCancel           pr_db.PatronRequestAction = "reject-cancel"
	LenderActionCannotSupply           pr_db.PatronRequestAction = "cannot-supply"
	LenderActionAddCondition           pr_db.PatronRequestAction = "add-condition"
	LenderActionAddItem                pr_db.PatronRequestAction = "add-item"
	LenderActionRemoveItem             pr_db.PatronRequestAction = "remove-item"
	LenderActionShip                   pr_db.PatronRequestAction = "ship"
	LenderActionSupplyDocument         pr_db.PatronRequestAction = "supply-document"
	LenderActionMarkReceived           pr_db.PatronRequestAction = "mark-received"
	LenderActionAcceptCancel           pr_db.PatronRequestAction = "accept-cancel"
	LenderActionAskRetry               pr_db.PatronRequestAction = "ask-retry"
	LenderActionSendNotification       pr_db.PatronRequestAction = "send-notification"

	TerminateAction pr_db.PatronRequestAction = "terminate"
)

const (
	SupplierExpectToSupply      MessageEvent = "expect-to-supply"
	SupplierExpectToSupplyLocal MessageEvent = "expect-to-supply-local"
	SupplierWillSupply          MessageEvent = "will-supply"
	SupplierWillSupplyCond      MessageEvent = "will-supply-conditional"
	SupplierLoaned              MessageEvent = "loaned"
	SupplierCompleted           MessageEvent = "completed"
	SupplierCompletedLocal      MessageEvent = "completed-local"
	SupplierUnfilled            MessageEvent = "unfilled"
	SupplierUnfilledLocal       MessageEvent = "unfilled-local"
	SupplierCancelledLocal      MessageEvent = "cancelled-local"
	SupplierCancelAccepted      MessageEvent = "cancel-accepted"
	SupplierCancelRejected      MessageEvent = "cancel-rejected"
	SupplierRetryConditional    MessageEvent = "retry-conditional"
	RequesterCancelRequest      MessageEvent = "cancel-request"
	RequesterReceived           MessageEvent = "received"
	RequesterShippedReturn      MessageEvent = "shipped-return"
	RequesterCondAccepted       MessageEvent = "conditions-accepted"
	RequesterCondRejected       MessageEvent = "condition-rejected"
)

func requesterBuiltInStates() []string {
	return uniqueSorted([]string{
		string(BorrowerStateNew),
		string(BorrowerStateInvalidPatron),
		string(BorrowerStateValidated),
		string(BorrowerStateMetadataUpdated),
		string(BorrowerStateNeedsReview),
		string(BorrowerStateReadyToSend),
		string(BorrowerStateLocalSupply),
		string(BorrowerStateSent),
		string(BorrowerStateSupplierLocated),
		string(BorrowerStateConditionPending),
		string(BorrowerStateWillSupply),
		string(BorrowerStateShipped),
		string(BorrowerStateReceived),
		string(BorrowerStateCheckedOut),
		string(BorrowerStateCheckedIn),
		string(BorrowerStateShippedReturned),
		string(BorrowerStateCancelPending),
		string(BorrowerStateCompleted),
		string(BorrowerStateCancelled),
		string(BorrowerStateUnfilled),
		string(BorrowerStateRetryAccepted),
		string(BorrowerStateRetryRejected),
		string(BorrowerStateRetryPending),
		string(BorrowerStateDuplicate),
		string(BorrowerStateManuallyClosed),
		string(BorrowerStateClosedDuplicate),
	})
}

func supplierBuiltInStates() []string {
	return uniqueSorted([]string{
		string(LenderStateNew),
		string(LenderStateValidated),
		string(LenderStateItemPending),
		string(LenderStateWillSupplyPending),
		string(LenderStateWillSupply),
		string(LenderStateConditionPending),
		string(LenderStateConditionAccepted),
		string(LenderStateShipped),
		string(LenderStateReceived),
		string(LenderStateShippedReturn),
		string(LenderStateCancelRequested),
		string(LenderStateCompleted),
		string(LenderStateCancelled),
		string(LenderStateUnfilled),
		string(LenderStateCompletedWithRetry),
		string(LenderStateManuallyClosed),
	})
}

func requesterBuiltInActions() []proapi.ActionCapability {
	actions := []proapi.ActionCapability{
		{
			Name:       string(BorrowerActionValidatePatron),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionUpdateMetadata),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionCheckDuplicate),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionSendRequest),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionCancelRequest),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionAcceptCondition),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionRejectCondition),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionReceive),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionCheckOut),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionCheckIn),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionShipReturn),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionAcceptRetry),
			Parameters: []string{},
		},
		transitionActionCapability(BorrowerActionRejectRetry),
		{
			Name:       string(BorrowerActionSendNotification),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionFillLocally),
			Parameters: []string{},
		},
		{
			Name: string(BorrowerActionSupplyDocument),
			Parameters: []string{
				"note",
				"deliveryUrl",
			},
		},
		{
			Name:       string(BorrowerActionCancelLocalSupply),
			Parameters: []string{},
		},
		{
			Name:       string(BorrowerActionCannotSupplyLocally),
			Parameters: []string{},
		},
		transitionActionCapability(BorrowerActionSkipPatronValidation),
		transitionActionCapability(BorrowerActionSkipMetadataUpdate),
		transitionActionCapability(BorrowerActionCloseRequest),
	}
	return actions
}

func transitionActionCapability(name pr_db.PatronRequestAction) proapi.ActionCapability {
	kind := proapi.Transition
	return proapi.ActionCapability{
		Name:       string(name),
		Parameters: []string{},
		Kind:       &kind,
	}
}

func isTransitionCapability(capability proapi.ActionCapability) bool {
	return capability.Kind != nil && *capability.Kind == proapi.Transition
}

var actionCapabilitiesBySide = map[pr_db.PatronRequestSide]map[pr_db.PatronRequestAction]proapi.ActionCapability{
	SideBorrowing: indexActionCapabilities(requesterBuiltInActions()),
	SideLending:   indexActionCapabilities(supplierBuiltInActions()),
}

func indexActionCapabilities(capabilities []proapi.ActionCapability) map[pr_db.PatronRequestAction]proapi.ActionCapability {
	indexed := make(map[pr_db.PatronRequestAction]proapi.ActionCapability, len(capabilities))
	for _, capability := range capabilities {
		indexed[pr_db.PatronRequestAction(capability.Name)] = capability
	}
	return indexed
}

func getActionCapability(side pr_db.PatronRequestSide, action pr_db.PatronRequestAction) (proapi.ActionCapability, bool) {
	capabilities, ok := actionCapabilitiesBySide[side]
	if !ok {
		return proapi.ActionCapability{}, false
	}
	capability, ok := capabilities[action]
	return capability, ok
}

func supplierBuiltInActions() []proapi.ActionCapability {
	return []proapi.ActionCapability{
		{
			Name:       string(LenderActionValidatePatron),
			Parameters: []string{},
		},
		{
			Name:       string(LenderActionRequestItem),
			Parameters: []string{},
		},
		{
			Name: string(LenderActionWillSupply),
			Parameters: []string{
				"note",
			},
		},
		{
			Name:       string(LenderActionRejectCancel),
			Parameters: []string{},
		},
		{
			Name: string(LenderActionCannotSupply),
			Parameters: []string{
				"note",
				"reasonUnfilled",
			},
		},
		{
			Name: string(LenderActionAddCondition),
			Parameters: []string{
				"note",
				"loanCondition",
				"cost",
				"currency",
			},
		},
		{
			Name: string(LenderActionAddItem),
			Parameters: []string{
				"barcode",
				"callNumber",
				"title",
				"itemId",
			},
		},
		{
			Name: string(LenderActionRemoveItem),
			Parameters: []string{
				"barcode",
			},
		},
		{
			Name: string(LenderActionShip),
			Parameters: []string{
				"note",
			},
		},
		{
			Name: string(LenderActionSupplyDocument),
			Parameters: []string{
				"note",
				"deliveryUrl",
			},
		},
		{
			Name:       string(LenderActionMarkReceived),
			Parameters: []string{},
		},
		{
			Name:       string(LenderActionAcceptCancel),
			Parameters: []string{},
		},
		{
			Name:       string(LenderActionSendNotification),
			Parameters: []string{},
		},
		{
			Name: string(LenderActionAskRetry),
			Parameters: []string{
				"note",
				"reasonRetry",
				"itemId",
			},
		},
	}
}

func requesterBuiltInMessageEvents() []string {
	return uniqueSorted([]string{
		string(RequesterCancelRequest),
		string(RequesterReceived),
		string(RequesterShippedReturn),
		string(RequesterCondAccepted),
		string(RequesterCondRejected),
	})
}

func supplierBuiltInMessageEvents() []string {
	return uniqueSorted([]string{
		string(SupplierExpectToSupply),
		string(SupplierExpectToSupplyLocal),
		string(SupplierWillSupply),
		string(SupplierWillSupplyCond),
		string(SupplierLoaned),
		string(SupplierCompleted),
		string(SupplierCompletedLocal),
		string(SupplierUnfilled),
		string(SupplierUnfilledLocal),
		string(SupplierCancelledLocal),
		string(SupplierCancelAccepted),
		string(SupplierCancelRejected),
		string(SupplierRetryConditional),
	})
}

func BuiltInStateModelCapabilities() proapi.StateModelCapabilities {
	return proapi.StateModelCapabilities{
		RequesterActions:       requesterBuiltInActions(),
		RequesterMessageEvents: requesterBuiltInMessageEvents(),
		RequesterStates:        requesterBuiltInStates(),
		SupplierActions:        supplierBuiltInActions(),
		SupplierMessageEvents:  supplierBuiltInMessageEvents(),
		SupplierStates:         supplierBuiltInStates(),
	}
}

func uniqueSorted(values []string) []string {
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(unique, value) {
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
}
