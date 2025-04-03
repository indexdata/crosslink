package iso18626

type ReasonRetry string

// for now, a tiny subset of https://illtransactions.org/opencode/2017/
const (
	ReasonRetryCostExceedsMaxCost ReasonRetry = "CostExceedsMaxCost"
	ReasonRetryOnLoan             ReasonRetry = "OnLoan"
	ReasonRetryLoanCondition      ReasonRetry = "LoanCondition"
)

type SentVia string

const (
	SentViaMail  SentVia = "Mail"
	SentViaEmail SentVia = "Email"
	SentViaFtp   SentVia = "FTP"
	SentViaUrl   SentVia = "URL"
)

type ElectronicAddressType string

const (
	ElectronicAddressTypeEmail ElectronicAddressType = "Email"
	ElectronicAddressTypeFtp   ElectronicAddressType = "FTP"
)

type Format string

const (
	FormatPdf       Format = "PDF"
	FormatPrinted   Format = "Printed"
	FormatPaperCopy Format = "PaperCopy"
)
