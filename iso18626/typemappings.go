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

var publicationTypeMap = map[string]PublicationType{
	strings.ToLower(string(PublicationTypeArchiveMaterial)): PublicationTypeArchiveMaterial,
	strings.ToLower(string(PublicationTypeArticle)):         PublicationTypeArticle,
	strings.ToLower(string(PublicationTypeAudioBook)):       PublicationTypeAudioBook,
	strings.ToLower(string(PublicationTypeBook)):            PublicationTypeBook,
	strings.ToLower(string(PublicationTypeChapter)):         PublicationTypeChapter,
	strings.ToLower(string(PublicationTypeConferenceProc)):  PublicationTypeConferenceProc,
	strings.ToLower(string(PublicationTypeGame)):            PublicationTypeGame,
	strings.ToLower(string(PublicationTypeGovernmentPubl)):  PublicationTypeGovernmentPubl,
	strings.ToLower(string(PublicationTypeImage)):           PublicationTypeImage,
	strings.ToLower(string(PublicationTypeJournal)):         PublicationTypeJournal,
	strings.ToLower(string(PublicationTypeManuscript)):      PublicationTypeManuscript,
	strings.ToLower(string(PublicationTypeMap)):             PublicationTypeMap,
	strings.ToLower(string(PublicationTypeMovie)):           PublicationTypeMovie,
	strings.ToLower(string(PublicationTypeMusicRecording)):  PublicationTypeMusicRecording,
	strings.ToLower(string(PublicationTypeMusicScore)):      PublicationTypeMusicScore,
	strings.ToLower(string(PublicationTypeNewspaper)):       PublicationTypeNewspaper,
	strings.ToLower(string(PublicationTypePatent)):          PublicationTypePatent,
	strings.ToLower(string(PublicationTypeReport)):          PublicationTypeReport,
	strings.ToLower(string(PublicationTypeSoundRecording)):  PublicationTypeSoundRecording,
	strings.ToLower(string(PublicationTypeThesis)):          PublicationTypeThesis,
}

// PublicationTypeFromStringCI converts a string to a PublicationType. Case-insensitive.
func PublicationTypeFromStringCI(s string) (PublicationType, bool) {
	pubType, ok := publicationTypeMap[strings.ToLower(s)]
	return pubType, ok
}

var loanConditionMap = map[string]LoanCondition{
	strings.ToLower(string(LoanConditionLibraryUseOnly)):      LoanConditionLibraryUseOnly,
	strings.ToLower(string(LoanConditionNoReproduction)):      LoanConditionNoReproduction,
	strings.ToLower(string(LoanConditionSignatureRequired)):   LoanConditionSignatureRequired,
	strings.ToLower(string(LoanConditionSpecCollSupervReq)):   LoanConditionSpecCollSupervReq,
	strings.ToLower(string(LoanConditionWatchLibraryUseOnly)): LoanConditionWatchLibraryUseOnly,
}

// LoanConditionFromStringCI converts a string to a LoanCondition. Case-insensitive.
func LoanConditionFromStringCI(s string) (LoanCondition, bool) {
	condition, ok := loanConditionMap[strings.ToLower(s)]
	return condition, ok
}
