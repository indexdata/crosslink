package ncipclient

import "github.com/indexdata/crosslink/ncip"

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
