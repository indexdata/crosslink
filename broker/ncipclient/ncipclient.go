package ncipclient

import "github.com/indexdata/crosslink/ncip"

// NcipClient defines the interface for NCIP operations
// customData is from the DirectoryEntry.CustomData field
type NcipClient interface {
	// AuthenticateUser performs user authentication
	// Returns true if authentication is successful (disabled or auto and NCIP lookup succeeded)
	// Returns false if authentication is manual
	// Returns an error otherwise (failed NCIP lookup, misconfiguration, etc)
	AuthenticateUser(customData map[string]any, arg ncip.LookupUser) (bool, error)
	AcceptItem(customData map[string]any, arg ncip.AcceptItem) (bool, error)
	CheckOutItem(customData map[string]any, arg ncip.CheckOutItem) error
	CheckInItem(customData map[string]any, arg ncip.CheckInItem) error
	DeleteItem(customData map[string]any, arg ncip.DeleteItem) error
	RequestItem(customData map[string]any, arg ncip.RequestItem) error
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
