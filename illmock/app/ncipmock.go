package app

import (
	"encoding/xml"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/ncip"
)

func setProblem(msg ncip.ProblemTypeMessage, detail string) []ncip.Problem {
	return []ncip.Problem{
		{
			ProblemType:   ncip.SchemeValuePair{Text: string(msg)},
			ProblemDetail: detail,
		},
	}
}

func handleLookupUser(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	if req.LookupUser.UserId == nil && len(req.LookupUser.AuthenticationInput) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	res.LookupUserResponse = &ncip.LookupUserResponse{}
	res.LookupUserResponse.UserId = req.LookupUser.UserId
	if problem == nil && req.LookupUser.UserId != nil && strings.HasPrefix(req.LookupUser.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.LookupUser.UserId.UserIdentifierValue)
	}
	res.LookupUserResponse.Problem = problem
}

func handleAcceptItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.AcceptItemResponse = &ncip.AcceptItemResponse{}
	if req.AcceptItem.RequestId.RequestIdentifierValue == "" {
		problem = setProblem(ncip.NeededDataMissing, "RequestId is required")
	}
	if problem == nil && req.AcceptItem.UserId != nil && strings.HasPrefix(req.AcceptItem.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.AcceptItem.UserId.UserIdentifierValue)
	}
	if problem == nil && req.AcceptItem.ItemId != nil && strings.HasPrefix(req.AcceptItem.ItemId.ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.AcceptItem.ItemId.ItemIdentifierValue)
	}
	res.AcceptItemResponse.RequestId = &req.AcceptItem.RequestId
	res.AcceptItemResponse.ItemId = req.AcceptItem.ItemId
	res.AcceptItemResponse.Problem = problem
}

func handleDeleteItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.DeleteItemResponse = &ncip.DeleteItemResponse{}
	if strings.HasPrefix(req.DeleteItem.ItemId.ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.DeleteItem.ItemId.ItemIdentifierValue)
	}
	res.DeleteItemResponse.ItemId = &req.DeleteItem.ItemId
	res.DeleteItemResponse.Problem = problem
}

func handleRequestItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.RequestItemResponse = &ncip.RequestItemResponse{}
	if req.RequestItem.UserId == nil && len(req.RequestItem.AuthenticationInput) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	if len(req.RequestItem.BibliographicId) == 0 && len(req.RequestItem.ItemId) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "BibliographicId or ItemId is required")
	}
	if problem == nil && req.RequestItem.RequestType.Text == "" {
		problem = setProblem(ncip.NeededDataMissing, "RequestType is required")
	}
	if problem == nil && req.RequestItem.RequestScopeType.Text == "" {
		problem = setProblem(ncip.NeededDataMissing, "RequestScopeType is required")
	}
	if problem == nil && req.RequestItem.UserId != nil && strings.HasPrefix(req.RequestItem.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.RequestItem.UserId.UserIdentifierValue)
	}
	if problem == nil && len(req.RequestItem.ItemId) > 0 && strings.HasPrefix(req.RequestItem.ItemId[0].ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.RequestItem.ItemId[0].ItemIdentifierValue)
	}
	if len(req.RequestItem.ItemId) > 0 {
		res.RequestItemResponse.ItemId = &req.RequestItem.ItemId[0]
	}
	res.RequestItemResponse.RequestScopeType = &req.RequestItem.RequestScopeType
	res.RequestItemResponse.RequestType = &req.RequestItem.RequestType
	res.RequestItemResponse.RequestId = req.RequestItem.RequestId
	res.RequestItemResponse.UserId = req.RequestItem.UserId
	res.RequestItemResponse.Problem = problem
}

func handleCancelRequestItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.CancelRequestItemResponse = &ncip.CancelRequestItemResponse{}
	if req.CancelRequestItem.UserId == nil && len(req.CancelRequestItem.AuthenticationInput) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	if problem == nil && req.CancelRequestItem.RequestId == nil && req.CancelRequestItem.ItemId == nil {
		problem = setProblem(ncip.NeededDataMissing, "RequestId or ItemId is required")
	}
	if problem == nil && req.CancelRequestItem.RequestType.Text == "" {
		problem = setProblem(ncip.NeededDataMissing, "RequestType is required")
	}
	if problem == nil && req.CancelRequestItem.UserId != nil && strings.HasPrefix(req.CancelRequestItem.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.CancelRequestItem.UserId.UserIdentifierValue)
	}
	if problem == nil && req.CancelRequestItem.ItemId != nil && strings.HasPrefix(req.CancelRequestItem.ItemId.ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.CancelRequestItem.ItemId.ItemIdentifierValue)
	}
	res.CancelRequestItemResponse.ItemId = req.CancelRequestItem.ItemId
	res.CancelRequestItemResponse.RequestId = req.CancelRequestItem.RequestId
	res.CancelRequestItemResponse.UserId = req.CancelRequestItem.UserId
	res.CancelRequestItemResponse.Problem = problem
}

func handleCheckInItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.CheckInItemResponse = &ncip.CheckInItemResponse{}
	if req.CheckInItem.ItemId.ItemIdentifierValue == "" {
		problem = setProblem(ncip.NeededDataMissing, "ItemId is required")
	}
	if problem == nil && strings.HasPrefix(req.CheckInItem.ItemId.ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.CheckInItem.ItemId.ItemIdentifierValue)
	}
	res.CheckInItemResponse.ItemId = &req.CheckInItem.ItemId
	res.CheckInItemResponse.Problem = problem
}

func handleCheckOutItem(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.CheckOutItemResponse = &ncip.CheckOutItemResponse{}
	if req.CheckOutItem.UserId == nil && len(req.CheckOutItem.AuthenticationInput) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	if req.CheckOutItem.ItemId.ItemIdentifierValue == "" {
		problem = setProblem(ncip.NeededDataMissing, "ItemId is required")
	}
	if problem == nil && req.CheckOutItem.UserId != nil && strings.HasPrefix(req.CheckOutItem.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.CheckOutItem.UserId.UserIdentifierValue)
	}
	if problem == nil && strings.HasPrefix(req.CheckOutItem.ItemId.ItemIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownItem, req.CheckOutItem.ItemId.ItemIdentifierValue)
	}
	res.CheckOutItemResponse.ItemId = &req.CheckOutItem.ItemId
	res.CheckOutItemResponse.UserId = req.CheckOutItem.UserId
	res.CheckOutItemResponse.Problem = problem
}

func handleCreateUserFiscalTransaction(req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	var problem []ncip.Problem
	res.CreateUserFiscalTransactionResponse = &ncip.CreateUserFiscalTransactionResponse{}
	if req.CreateUserFiscalTransaction.UserId == nil && len(req.CreateUserFiscalTransaction.AuthenticationInput) == 0 {
		problem = setProblem(ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	if problem == nil && req.CreateUserFiscalTransaction.FiscalTransactionInformation.FiscalActionType.Text == "" {
		problem = setProblem(ncip.NeededDataMissing, "FiscalTransactionInformation is required")
	}
	if problem == nil && req.CreateUserFiscalTransaction.UserId != nil && strings.HasPrefix(req.CreateUserFiscalTransaction.UserId.UserIdentifierValue, "f") {
		problem = setProblem(ncip.UnknownUser, req.CreateUserFiscalTransaction.UserId.UserIdentifierValue)
	}
	res.CreateUserFiscalTransactionResponse.UserId = req.CreateUserFiscalTransaction.UserId
	res.CreateUserFiscalTransactionResponse.FiscalTransactionReferenceId =
		req.CreateUserFiscalTransaction.FiscalTransactionInformation.FiscalTransactionReferenceId
	res.CreateUserFiscalTransactionResponse.Problem = problem
}

func ncipMockHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	if mediaType != "application/xml" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	byteReq, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var ncipRequest ncip.NCIPMessage
	var problem []ncip.Problem
	err = xml.Unmarshal(byteReq, &ncipRequest)
	if err != nil {
		problem = setProblem(ncip.InvalidMessageSyntaxError, err.Error())
	}
	if problem == nil && ncipRequest.Version == "" {
		problem = setProblem(ncip.MissingVersion, "")
	}
	var ncipResponse = ncip.NCIPMessage{
		Version: ncipRequest.Version,
	}
	switch {
	case problem != nil:
		ncipResponse.Problem = problem
	case ncipRequest.LookupUser != nil:
		handleLookupUser(&ncipRequest, &ncipResponse)
	case ncipRequest.AcceptItem != nil:
		handleAcceptItem(&ncipRequest, &ncipResponse)
	case ncipRequest.DeleteItem != nil:
		handleDeleteItem(&ncipRequest, &ncipResponse)
	case ncipRequest.RequestItem != nil:
		handleRequestItem(&ncipRequest, &ncipResponse)
	case ncipRequest.CancelRequestItem != nil:
		handleCancelRequestItem(&ncipRequest, &ncipResponse)
	case ncipRequest.CheckInItem != nil:
		handleCheckInItem(&ncipRequest, &ncipResponse)
	case ncipRequest.CheckOutItem != nil:
		handleCheckOutItem(&ncipRequest, &ncipResponse)
	case ncipRequest.CreateUserFiscalTransaction != nil:
		handleCreateUserFiscalTransaction(&ncipRequest, &ncipResponse)
	default:
		ncipResponse.Problem = setProblem(ncip.UnsupportedService, "")
	}
	bytesResponse, err := xml.MarshalIndent(ncipResponse, "", "  ")
	if err != nil {
		http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	netutil.WriteHttpResponse(w, bytesResponse)
}
