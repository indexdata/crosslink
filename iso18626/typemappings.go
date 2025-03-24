package iso18626

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
