package events

import extctx "github.com/indexdata/crosslink/broker/common"

func LogErrorAndReturnResult(ctx extctx.ExtendedContext, message string, err error) (EventStatus, *EventResult) {
	ctx.Logger().Error(message, "error", err)
	cause := ""
	if err != nil {
		cause = err.Error()
	}
	return NewErrorResult(message, cause)
}

func LogErrorAndReturnExistingResult(ctx extctx.ExtendedContext, message string, err error, existingResult *EventResult) (EventStatus, *EventResult) {
	ctx.Logger().Error(message, "error", err)
	cause := ""
	if err != nil {
		cause = err.Error()
	}
	existingResult.EventError = &EventError{
		Message: message,
		Cause:   cause,
	}
	return EventStatusError, existingResult
}

func LogProblemAndReturnResult(ctx extctx.ExtendedContext, problem string, message string, customResult map[string]any) (EventStatus, *EventResult) {
	ctx.Logger().Info(message, "problem", problem)
	status, result := NewProblemResult(problem, message)
	if customResult != nil {
		result.CustomData = customResult
	}
	return status, result
}
