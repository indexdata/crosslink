package events

import extctx "github.com/indexdata/crosslink/broker/common"

func LogErrorAndReturnResult(ctx extctx.ExtendedContext, component string, message string, err error) (EventStatus, *EventResult) {
	ctx.Logger().Error(message, "error", err, "component", component)
	return NewErrorResult(message, err.Error())
}

func LogProblemAndReturnResult(ctx extctx.ExtendedContext, component string, problem string, message string, customResult map[string]any) (EventStatus, *EventResult) {
	ctx.Logger().Debug(message, "component", component)
	status, result := NewProblemResult(problem, message)
	if customResult != nil {
		result.CustomData = customResult
	}
	return status, result
}
