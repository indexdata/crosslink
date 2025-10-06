package app

import (
	"encoding/xml"
	"io"
	"net/http"

	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/ncip"
)

func addProblem(problems []ncip.Problem, msg ncip.ProblemTypeMessage, detail string) []ncip.Problem {
	// only first problem reported
	if len(problems) > 0 {
		return problems
	}
	return append(problems, ncip.Problem{
		ProblemType:   ncip.SchemeValuePair{Text: string(msg)},
		ProblemDetail: detail,
	})
}

func handleLookupUser(problems []ncip.Problem, req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	res.Version = req.Version
	if req.LookupUser.UserId == nil && len(req.LookupUser.AuthenticationInput) == 0 {
		problems = addProblem(problems, ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
	}
	res.LookupUserResponse = &ncip.LookupUserResponse{}
	res.LookupUserResponse.UserId = req.LookupUser.UserId
	res.LookupUserResponse.Problem = problems
}

func ncipMockHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/xml" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	byteReq, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var ncipRequest ncip.NCIPMessage
	var problems []ncip.Problem
	err = xml.Unmarshal(byteReq, &ncipRequest)
	if err != nil {
		problems = addProblem(problems, ncip.InvalidMessageSyntaxError, err.Error())
	}
	if ncipRequest.Version == "" {
		problems = addProblem(problems, ncip.MissingVersion, "")
	}
	var ncipResponse = ncip.NCIPMessage{
		Version: ncipRequest.Version,
	}
	// LookupUser
	// AcceptItem
	// DeleteItem
	// RequestItem
	// CancelRequestItem
	// CheckInItem
	// CheckOutItem
	// CreateUserFiscalTransaction (fees and fines)

	switch {
	case ncipRequest.LookupUser != nil:
		handleLookupUser(problems, &ncipRequest, &ncipResponse)
	default:
		problems = addProblem(problems, ncip.UnsupportedService, "")
		ncipResponse.Problem = problems
	}
	bytesResponse, err := xml.MarshalIndent(ncipResponse, "", "  ")
	if err != nil {
		http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	netutil.WriteHttpResponse(w, bytesResponse)
}

func getNcipMockHandler() http.HandlerFunc {
	return ncipMockHandler
}
