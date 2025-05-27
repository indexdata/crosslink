package iso18626

import "strings"

var StatusMap = map[string]TypeStatus{
	string(TypeStatusRequestReceived):        TypeStatusRequestReceived,
	string(TypeStatusExpectToSupply):         TypeStatusExpectToSupply,
	string(TypeStatusWillSupply):             TypeStatusWillSupply,
	string(TypeStatusLoaned):                 TypeStatusLoaned,
	string(TypeStatusOverdue):                TypeStatusOverdue,
	string(TypeStatusRecalled):               TypeStatusRecalled,
	string(TypeStatusRetryPossible):          TypeStatusRetryPossible,
	string(TypeStatusUnfilled):               TypeStatusUnfilled,
	string(TypeStatusCopyCompleted):          TypeStatusCopyCompleted,
	string(TypeStatusLoanCompleted):          TypeStatusLoanCompleted,
	string(TypeStatusCompletedWithoutReturn): TypeStatusCompletedWithoutReturn,
	string(TypeStatusCancelled):              TypeStatusCancelled,
}

var ActionMap = map[string]TypeAction{
	string(TypeActionStatusRequest):  TypeActionStatusRequest,
	string(TypeActionReceived):       TypeActionReceived,
	string(TypeActionCancel):         TypeActionCancel,
	string(TypeActionRenew):          TypeActionRenew,
	string(TypeActionShippedReturn):  TypeActionShippedReturn,
	string(TypeActionShippedForward): TypeActionShippedForward,
	string(TypeActionNotification):   TypeActionNotification,
}

var ReasonForMassageMap = map[string]TypeReasonForMessage{
	string(TypeReasonForMessageRequestResponse):       TypeReasonForMessageRequestResponse,
	string(TypeReasonForMessageStatusRequestResponse): TypeReasonForMessageStatusRequestResponse,
	string(TypeReasonForMessageRenewResponse):         TypeReasonForMessageRenewResponse,
	string(TypeReasonForMessageCancelResponse):        TypeReasonForMessageCancelResponse,
	string(TypeReasonForMessageStatusChange):          TypeReasonForMessageStatusChange,
	string(TypeReasonForMessageNotification):          TypeReasonForMessageNotification,
}

var serviceLevelMap = map[string]ServiceLevel{
	strings.ToLower(string(ServiceLevelExpress)):       ServiceLevelExpress,
	strings.ToLower(string(ServiceLevelNormal)):        ServiceLevelNormal,
	strings.ToLower(string(ServiceLevelRush)):          ServiceLevelRush,
	strings.ToLower(string(ServiceLevelSecondaryMail)): ServiceLevelSecondaryMail,
	strings.ToLower(string(ServiceLevelStandard)):      ServiceLevelStandard,
	strings.ToLower(string(ServiceLevelUrgent)):        ServiceLevelUrgent,
}

// ServiceLevelFromStringCI converts a string to a ServiceLevel. Case-insensitive.
func ServiceLevelFromStringCI(s string) (ServiceLevel, bool) {
	level, ok := serviceLevelMap[strings.ToLower(s)]
	return level, ok
}
