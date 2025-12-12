package ncipclient

import "github.com/indexdata/crosslink/ncip"

type NcipMode string

const (
	ModeDisabled NcipMode = "disabled"
	ModeAuto     NcipMode = "auto"
	ModeManual   NcipMode = "manual"
)

type NcipProperty string

const (
	Ncip                            NcipProperty = "ncip"
	FromAgency                      NcipProperty = "from_agency"
	FromAgencyAuthentication        NcipProperty = "from_agency_authentication"
	ToAgency                        NcipProperty = "to_agency"
	Address                         NcipProperty = "address"
	LookupUserMode                  NcipProperty = "lookup_user_mode"
	AcceptItemMode                  NcipProperty = "accept_item_mode"
	RequestItemMode                 NcipProperty = "request_item_mode"
	CreateUserFiscalTransactionMode NcipProperty = "create_user_fiscal_transaction_mode"
)

type NcipClient interface {
	LookupUser(arg ncip.LookupUser) (*ncip.LookupUserResponse, error)

	AcceptItem(arg ncip.AcceptItem) (*ncip.AcceptItemResponse, error)

	DeleteItem(arg ncip.DeleteItem) (*ncip.DeleteItemResponse, error)

	RequestItem(arg ncip.RequestItem) (*ncip.RequestItemResponse, error)

	CancelRequestItem(arg ncip.CancelRequestItem) (*ncip.CancelRequestItemResponse, error)

	CheckInItem(arg ncip.CheckInItem) (*ncip.CheckInItemResponse, error)

	CheckOutItem(arg ncip.CheckOutItem) (*ncip.CheckOutItemResponse, error)

	CreateUserFiscalTransaction(arg ncip.CreateUserFiscalTransaction) (*ncip.CreateUserFiscalTransactionResponse, error)
}

type NcipError struct {
	Message string
	Problem ncip.Problem
}

func (e *NcipError) Error() string {
	s := e.Message
	if e.Problem.ProblemType.Text != "" {
		s += ": " + e.Problem.ProblemType.Text
	}
	if e.Problem.ProblemDetail != "" {
		s += ": " + e.Problem.ProblemDetail
	}
	return s
}
