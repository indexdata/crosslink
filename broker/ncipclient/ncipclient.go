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

// NcipClient defines the interface for NCIP operations
// customData is from the DirectoryEntry.CustomData field
type NcipClient interface {
	// LookupUser performs user authentication.
	// Returns true if authentication is successful (disabled or auto and NCIP lookup succeeded)
	// Returns false if authentication is manual
	// Returns an error otherwise (failed NCIP lookup, misconfiguration, etc)
	LookupUser(customData map[string]any, arg ncip.LookupUser) (bool, error)

	// AcceptItem accepts an item.
	// Returns true if accept is successful (disabled or auto and NCIP accept succeeded)
	// Returns false if accept is manual
	// Returns an error otherwise (failed NCIP accept, misconfiguration, etc)
	AcceptItem(customData map[string]any, arg ncip.AcceptItem) (bool, error)

	DeleteItem(customData map[string]any, arg ncip.DeleteItem) error

	// RequestItem requests an item.
	// Returns true if request is successful (disabled or auto and NCIP request succeeded)
	// Returns false if request is manual
	// Returns an error otherwise (failed NCIP request, misconfiguration, etc)
	RequestItem(customData map[string]any, arg ncip.RequestItem) (bool, error)

	CancelRequestItem(customData map[string]any, arg ncip.CancelRequestItem) error

	CheckInItem(customData map[string]any, arg ncip.CheckInItem) error

	CheckOutItem(customData map[string]any, arg ncip.CheckOutItem) error

	// CreateUserFiscalTransaction creates a user fiscal transaction.
	// Returns true if creation is successful (disabled or auto and NCIP creation succeeded)
	// Returns false if creation is manual
	// Returns an error otherwise (failed NCIP creation, misconfiguration, etc)
	CreateUserFiscalTransaction(customData map[string]any, arg ncip.CreateUserFiscalTransaction) (bool, error)
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
