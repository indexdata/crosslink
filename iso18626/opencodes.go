package iso18626

type ReasonRetry string

// for now, a tiny subset of https://illtransactions.org/opencode/2017/
const (
	ReasonRetryCostExceedsMaxCost ReasonRetry = "CostExceedsMaxCost"
	ReasonRetryOnLoan             ReasonRetry = "OnLoan"
	ReasonRetryLoanCondition      ReasonRetry = "LoanCondition"
)
