package ncipclientv1

import (
	"fmt"
	"strings"

	ncip "github.com/indexdata/crosslink/ncipv1"
)

// Validate the acknowledgement structure before treating a response as success.
// Optional metadata is decoded normally; this is not a general XSD validator.
func validateResponse(message *ncip.NCIPMessage, fields map[string]int) error {
	var header ncip.ResponseHeader
	var problem *ncip.Problem
	var user *ncip.UniqueUserId
	var item *ncip.UniqueItemId
	var request *ncip.UniqueRequestId
	var required []string
	var requestType *ncip.RequestType
	var scope *ncip.RequestScopeType
	var requestChoice, loanPeriod, feeProblem bool
	switch {
	case message.LookupUserResponse != nil:
		r := message.LookupUserResponse
		header, problem, user = r.ResponseHeader, r.Problem, r.UniqueUserId
		required = []string{"UniqueUserId"}
	case message.AcceptItemResponse != nil:
		r := message.AcceptItemResponse
		header, problem, request, item = r.ResponseHeader, r.Problem, r.UniqueRequestId, r.UniqueItemId
		required = []string{"UniqueRequestId"}
	case message.CheckInItemResponse != nil:
		r := message.CheckInItemResponse
		header, problem, item, user = r.ResponseHeader, r.Problem, r.UniqueItemId, r.UniqueUserId
		required = []string{"UniqueItemId"}
	case message.CheckOutItemResponse != nil:
		r := message.CheckOutItemResponse
		header, problem, item, user = r.ResponseHeader, r.Problem, r.UniqueItemId, r.UniqueUserId
		required = []string{"UniqueItemId", "UniqueUserId"}
		loanPeriod, feeProblem = true, true
	case message.RequestItemResponse != nil:
		r := message.RequestItemResponse
		header, problem, item, user, request = r.ResponseHeader, r.Problem, r.UniqueItemId, r.UniqueUserId, r.UniqueRequestId
		required = []string{"UniqueUserId", "RequestType", "RequestScopeType"}
		requestType, scope = r.RequestType, r.RequestScopeType
		requestChoice, feeProblem = true, true
	case message.CancelRequestItemResponse != nil:
		r := message.CancelRequestItemResponse
		header, problem, item, user, request = r.ResponseHeader, r.Problem, r.UniqueItemId, r.UniqueUserId, r.UniqueRequestId
		required = []string{"UniqueUserId"}
		requestChoice = true
	default:
		// The calling service reports the missing expected response.
		return nil
	}
	invalid := func(field string) error { return fmt.Errorf("invalid NCIP v1 response: %s", field) }
	for name, count := range fields {
		if count > 1 && name != "RequiredItemUseRestrictionType" && name != "UserFiscalAccount" && name != "LoanedItemsCount" && name != "RequestedItemsCount" {
			return invalid("duplicate " + name)
		}
	}
	if fields["ResponseHeader"] != 1 || !validAgency(header.FromAgencyId.UniqueAgencyId) || !validAgency(header.ToAgencyId.UniqueAgencyId) {
		return invalid("missing or incomplete ResponseHeader")
	}
	if problem != nil {
		for field := range fields {
			if field == "ResponseHeader" || field == "Problem" || (feeProblem && (field == "RequiredFeeAmount" || field == "RequiredItemUseRestrictionType")) {
				continue
			}
			return invalid("Problem mixed with success payload")
		}
		return validateProblem(problem)
	}
	if fields["RequiredFeeAmount"] != 0 || fields["RequiredItemUseRestrictionType"] != 0 {
		return invalid("required fees or restrictions without Problem")
	}
	for _, field := range required {
		if fields[field] != 1 {
			return invalid("missing " + field)
		}
	}
	if requestChoice && request == nil && item == nil {
		return invalid("missing UniqueRequestId or UniqueItemId")
	}
	if user != nil && (!validAgency(user.UniqueAgencyId) || strings.TrimSpace(user.UserIdentifierValue) == "") {
		return invalid("incomplete UniqueUserId")
	}
	if item != nil && (!validAgency(item.UniqueAgencyId) || strings.TrimSpace(item.ItemIdentifierValue) == "") {
		return invalid("incomplete UniqueItemId")
	}
	if request != nil && (!validAgency(request.UniqueAgencyId) || strings.TrimSpace(request.RequestIdentifierValue) == "") {
		return invalid("incomplete UniqueRequestId")
	}
	if requestType != nil && !validPair(requestType.Scheme, requestType.Value) {
		return invalid("incomplete RequestType")
	}
	if scope != nil && !validPair(scope.Scheme, scope.Value) {
		return invalid("incomplete RequestScopeType")
	}
	// Preserve the established leniency for unusable DateDue values after a
	// confirmed checkout, but require exactly one loan-period element.
	if loanPeriod && fields["DateDue"]+fields["IndeterminateLoanPeriodFlag"]+fields["NonReturnableFlag"] != 1 {
		return invalid("expected one loan period")
	}
	return nil
}

func validPair(scheme, value string) bool {
	return strings.TrimSpace(scheme) != "" && strings.TrimSpace(value) != ""
}
func validAgency(agency ncip.UniqueAgencyId) bool { return validPair(agency.Scheme, agency.Value) }

func validateProblem(problem *ncip.Problem) error {
	invalid := func() error { return fmt.Errorf("invalid NCIP v1 response: incomplete or mixed Problem") }
	if (len(problem.MessagingError) == 0) == (len(problem.ProcessingError) == 0) {
		return invalid()
	}
	for _, p := range problem.MessagingError {
		if !validPair(p.MessagingErrorType.Scheme, p.MessagingErrorType.Value) {
			return invalid()
		}
		for e := &p.MessagingErrorElement; e != nil; e = e.MessagingErrorElement {
			if strings.TrimSpace(e.ElementName) == "" || (e.MessagingErrorElement != nil && e.MessagingErrorValue != "") {
				return invalid()
			}
		}
	}
	for _, p := range problem.ProcessingError {
		if !validPair(p.ProcessingErrorType.Scheme, p.ProcessingErrorType.Value) {
			return invalid()
		}
		for e := &p.ProcessingErrorElement; e != nil; e = e.ProcessingErrorElement {
			if strings.TrimSpace(e.ElementName) == "" || (e.ProcessingErrorElement != nil && e.ProcessingErrorValue != "") {
				return invalid()
			}
		}
	}
	return nil
}
