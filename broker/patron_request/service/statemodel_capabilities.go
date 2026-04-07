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
)

const (
	SideBorrowing pr_db.PatronRequestSide = "borrowing"
	SideLending   pr_db.PatronRequestSide = "lending"
)

const (
	BorrowerStateNew              pr_db.PatronRequestState = "NEW"
	BorrowerStateValidated        pr_db.PatronRequestState = "VALIDATED"
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
	LenderStateNew                pr_db.PatronRequestState = "NEW"
	LenderStateValidated          pr_db.PatronRequestState = "VALIDATED"
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
)

const (
	BorrowerActionValidate        pr_db.PatronRequestAction = "validate"
	BorrowerActionSendRequest     pr_db.PatronRequestAction = "send-request"
	BorrowerActionCancelRequest   pr_db.PatronRequestAction = "cancel-request"
	BorrowerActionAcceptCondition pr_db.PatronRequestAction = "accept-condition"
	BorrowerActionRejectCondition pr_db.PatronRequestAction = "reject-condition"
	BorrowerActionReceive         pr_db.PatronRequestAction = "receive"
	BorrowerActionCheckOut        pr_db.PatronRequestAction = "check-out"
	BorrowerActionCheckIn         pr_db.PatronRequestAction = "check-in"
	BorrowerActionShipReturn      pr_db.PatronRequestAction = "ship-return"

	LenderActionValidate     pr_db.PatronRequestAction = "validate"
	LenderActionWillSupply   pr_db.PatronRequestAction = "will-supply"
	LenderActionRejectCancel pr_db.PatronRequestAction = "reject-cancel"
	LenderActionCannotSupply pr_db.PatronRequestAction = "cannot-supply"
	LenderActionAddCondition pr_db.PatronRequestAction = "add-condition"
	LenderActionShip         pr_db.PatronRequestAction = "ship"
	LenderActionMarkReceived pr_db.PatronRequestAction = "mark-received"
	LenderActionAcceptCancel pr_db.PatronRequestAction = "accept-cancel"
)

const (
	SupplierExpectToSupply MessageEvent = "expect-to-supply"
	SupplierWillSupply     MessageEvent = "will-supply"
	SupplierWillSupplyCond MessageEvent = "will-supply-conditional"
	SupplierLoaned         MessageEvent = "loaned"
	SupplierCompleted      MessageEvent = "completed"
	SupplierUnfilled       MessageEvent = "unfilled"
	SupplierCancelAccepted MessageEvent = "cancel-accepted"
	SupplierCancelRejected MessageEvent = "cancel-rejected"
	RequesterCancelRequest MessageEvent = "cancel-request"
	RequesterReceived      MessageEvent = "received"
	RequesterShippedReturn MessageEvent = "shipped-return"
	RequesterCondAccepted  MessageEvent = "conditions-accepted"
	RequesterCondRejected  MessageEvent = "condition-rejected"
)

func requesterBuiltInStates() []string {
	return uniqueSorted([]string{
		string(BorrowerStateNew),
		string(BorrowerStateValidated),
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
	})
}

func supplierBuiltInStates() []string {
	return uniqueSorted([]string{
		string(LenderStateNew),
		string(LenderStateValidated),
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
	})
}

func requesterBuiltInActions() []proapi.ActionCapability {
	return []proapi.ActionCapability{
		{
			Name:       string(BorrowerActionValidate),
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
	}
}

func supplierBuiltInActions() []proapi.ActionCapability {
	return []proapi.ActionCapability{
		{
			Name:       string(LenderActionValidate),
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
			Name: string(LenderActionShip),
			Parameters: []string{
				"note",
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
		string(SupplierWillSupply),
		string(SupplierWillSupplyCond),
		string(SupplierLoaned),
		string(SupplierCompleted),
		string(SupplierUnfilled),
		string(SupplierCancelAccepted),
		string(SupplierCancelRejected),
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
