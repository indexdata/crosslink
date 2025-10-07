package app

import (
	"encoding/xml"
	"io"
	"mime"
	"net/http"

	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/ncip"
)

func setProblem(problem []ncip.Problem, msg ncip.ProblemTypeMessage, detail string) []ncip.Problem {
	// only first problem reported
	if problem != nil {
		return problem
	}
	return []ncip.Problem{
		{
			ProblemType:   ncip.SchemeValuePair{Text: string(msg)},
			ProblemDetail: detail,
		},
	}
}

func handleLookupUser(problems []ncip.Problem, req *ncip.NCIPMessage, res *ncip.NCIPMessage) {
	res.Version = req.Version
	if req.LookupUser.UserId == nil && len(req.LookupUser.AuthenticationInput) == 0 {
		problems = setProblem(problems, ncip.NeededDataMissing, "UserId or AuthenticationInput is required")
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
		problem = setProblem(problem, ncip.InvalidMessageSyntaxError, err.Error())
	}
	if ncipRequest.Version == "" {
		problem = setProblem(problem, ncip.MissingVersion, "")
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
		handleLookupUser(problem, &ncipRequest, &ncipResponse)
	default:
		problem = setProblem(problem, ncip.UnsupportedService, "")
		ncipResponse.Problem = problem
	}
	bytesResponse, err := xml.MarshalIndent(ncipResponse, "", "  ")
	if err != nil {
		http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	netutil.WriteHttpResponse(w, bytesResponse)
}
