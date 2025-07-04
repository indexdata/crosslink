package app

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/illmock/dirmock"
	"github.com/indexdata/crosslink/illmock/flows"
	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/illmock/reqform"
	"github.com/indexdata/crosslink/illmock/role"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/crosslink/illmock/sruapi"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type MockApp struct {
	httpPort     string
	agencyType   string
	peerUrl      string
	messageDelay time.Duration
	server       *http.Server
	requester    Requester
	supplier     Supplier
	flowsApi     *flows.FlowsApi
	sruApi       *sruapi.SruApi
	headers      []string
	client       httpclient.HttpClient
}

var log *slog.Logger = slogwrap.SlogWrap()

func validateHeader(header *iso18626.Header) error {
	if header.RequestingAgencyRequestId == "" {
		return fmt.Errorf("RequestingAgencyRequestId cannot be empty")
	}
	if header.RequestingAgencyId.AgencyIdValue == "" {
		return fmt.Errorf("RequestingAgencyId cannot be empty")
	}
	if header.SupplyingAgencyId.AgencyIdValue == "" {
		return fmt.Errorf("SupplyingAgencyId cannot be empty")
	}
	return nil
}

func (app *MockApp) writeIso18626Response(resmsg *iso18626.Iso18626MessageNS, w http.ResponseWriter, role role.Role, header *iso18626.Header) {
	buf := utils.Must(xml.MarshalIndent(resmsg, "  ", "  "))
	if buf == nil {
		http.Error(w, "marshal failed", http.StatusInternalServerError)
		return
	}
	app.logOutgoingRes(role, header, resmsg)
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	w.Header().Set("X-Request-ID", header.RequestingAgencyRequestId)
	netutil.WriteHttpResponse(w, buf)
}

func createConfirmationHeader(inHeader *iso18626.Header, messageStatus iso18626.TypeMessageStatus) *iso18626.ConfirmationHeader {
	var header = &iso18626.ConfirmationHeader{}
	header.RequestingAgencyId = &iso18626.TypeAgencyId{}
	header.RequestingAgencyId.AgencyIdType = inHeader.RequestingAgencyId.AgencyIdType
	header.RequestingAgencyId.AgencyIdValue = inHeader.RequestingAgencyId.AgencyIdValue
	header.TimestampReceived = inHeader.Timestamp
	header.RequestingAgencyRequestId = inHeader.RequestingAgencyRequestId

	if len(inHeader.SupplyingAgencyId.AgencyIdValue) != 0 {
		header.SupplyingAgencyId = &iso18626.TypeAgencyId{}
		header.SupplyingAgencyId.AgencyIdType = inHeader.SupplyingAgencyId.AgencyIdType
		header.SupplyingAgencyId.AgencyIdValue = inHeader.SupplyingAgencyId.AgencyIdValue
	}

	header.Timestamp = utils.XSDDateTime{Time: time.Now()}
	header.MessageStatus = messageStatus
	return header
}

func createErrorData(errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ErrorData {
	if errorMessage != nil {
		var errorData = iso18626.ErrorData{
			ErrorType:  *errorType,
			ErrorValue: *errorMessage,
		}
		return &errorData
	}
	return nil
}

func createRequestResponse(requestHeader *iso18626.Header, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(requestHeader, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func (app *MockApp) handleRequestError(requestHeader *iso18626.Header, role role.Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(requestHeader, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	app.writeIso18626Response(resmsg, w, role, requestHeader)
}

func createRequestingAgencyConfirmation(iheader *iso18626.Header, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(iheader, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func (app *MockApp) sendReceive(url string, msg *iso18626.Iso18626MessageNS, role role.Role, header *iso18626.Header) (*iso18626.Iso18626MessageNS, error) {
	if url == "" {
		return nil, fmt.Errorf("url cannot be empty")
	}
	app.logOutgoingReq(role, header, msg, url)
	var response iso18626.Iso18626MessageNS
	err := app.client.PostXml(http.DefaultClient, url, msg, &response)
	if err != nil {
		status := 0
		if httpErr, ok := err.(*httpclient.HttpError); ok {
			status = httpErr.StatusCode
		}
		app.logOutgoingErr(role, header, url, status, err.Error())
		return nil, err
	}
	app.logIncomingRes(role, header, &response, url)
	return &response, nil
}

func logMessage(lead string, illMessage *iso18626.Iso18626MessageNS) bool {
	buf := utils.Must(xml.MarshalIndent(illMessage, "  ", "  "))
	if buf == nil {
		return false
	}
	log.Info(fmt.Sprintf("%s\n%s", lead, buf))
	return true
}

func (app *MockApp) logIso18626Message(role role.Role, kind string, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS, extra string) {
	logmsg := fmt.Sprintf("%s role:%s id:%s req:%s sup:%s%s", kind, role, header.RequestingAgencyRequestId,
		header.RequestingAgencyId.AgencyIdValue, header.SupplyingAgencyId.AgencyIdValue, extra)
	if logMessage(logmsg, illMessage) {
		flowMessage := flows.FlowMessage{Kind: kind, Timestamp: header.Timestamp, Message: *illMessage}
		app.flowsApi.AddFlow(flows.Flow{Message: []flows.FlowMessage{flowMessage}, Id: header.RequestingAgencyRequestId, Role: role,
			Supplier: header.SupplyingAgencyId.AgencyIdValue, Requester: header.RequestingAgencyId.AgencyIdValue})
	}
}

func (app *MockApp) logIncomingReq(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	app.logIso18626Message(role, "incoming-request", header, illMessage, "")
}

func (app *MockApp) logOutgoingReq(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS,
	url string) {
	app.logIso18626Message(role, "outgoing-request", header, illMessage, fmt.Sprintf(" url:%s", url))
}

func (app *MockApp) logOutgoingErr(role role.Role, header *iso18626.Header, url string, status int, error string) {
	log.Info(fmt.Sprintf("outgoing-error role:%s id:%s req:%s sup:%s url:%s status:%d error:%s", role, header.RequestingAgencyRequestId,
		header.RequestingAgencyId.AgencyIdValue, header.SupplyingAgencyId.AgencyIdValue, url, status, error))
}

func (app *MockApp) logIncomingRes(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS,
	url string) {
	app.logIso18626Message(role, "incoming-response", header, illMessage, fmt.Sprintf(" url:%s", url))
}

func (app *MockApp) logOutgoingRes(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	app.logIso18626Message(role, "outgoing-response", header, illMessage, "")
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		netutil.WriteHttpResponse(w, []byte("OK\r\n"))
	}
}

func error400Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad request", http.StatusBadRequest)
	}
}

func error500Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func iso18626Handler(app *MockApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get(httpclient.ContentType)
		if !strings.HasPrefix(contentType, httpclient.ContentTypeApplicationXml) && !strings.HasPrefix(contentType, httpclient.ContentTypeTextXml) {
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.Iso18626MessageNS
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			http.Error(w, "unmarshal: "+err.Error(), http.StatusBadRequest)
			return
		}
		if illMessage.Request != nil {
			illRequest := illMessage.Request
			if illRequest.ServiceInfo != nil {
				subtypes := illRequest.ServiceInfo.RequestSubType
				if slices.Contains(subtypes, iso18626.TypeRequestSubTypePatronRequest) {
					app.handlePatronRequest(&illMessage, w)
					return
				}
			}
			app.logIncomingReq(role.Supplier, &illRequest.Header, &illMessage)
			app.handleSupplierRequest(illRequest, w)
		} else if illMessage.RequestingAgencyMessage != nil {
			app.handleIso18626RequestingAgencyMessage(&illMessage, w)
		} else if illMessage.SupplyingAgencyMessage != nil {
			app.handleIso18626SupplyingAgencyMessage(&illMessage, w)
		} else {
			log.Warn("invalid ISO18626 message")
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func getMessageDelay(val string) (time.Duration, error) {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid MESSAGE_DELAY: %s", err.Error())
	}
	if d < 0 {
		return 0, errors.New("MESSAGE_DELAY can not be negative")
	}
	return d, nil
}

func (app *MockApp) parseEnv() error {
	if app.httpPort == "" {
		app.httpPort = utils.GetEnv("HTTP_PORT", "8081")
	}
	if app.agencyType == "" {
		app.agencyType = os.Getenv("AGENCY_TYPE")
	}
	if app.requester.supplyingAgencyId == "" {
		app.requester.supplyingAgencyId = os.Getenv("SUPPLYING_AGENCY_ID")
	}
	if app.requester.requestingAgencyId == "" {
		app.requester.requestingAgencyId = os.Getenv("REQUESTING_AGENCY_ID")
	}
	if app.peerUrl == "" {
		app.peerUrl = utils.GetEnv("PEER_URL", "http://localhost:8081/iso18626")
	}
	if app.messageDelay == 0 {
		d, err := getMessageDelay(utils.GetEnv("MESSAGE_DELAY", "100ms"))
		if err != nil {
			return err
		}
		app.messageDelay = d
	}
	if app.headers == nil {
		app.headers = parseKVs(utils.GetEnv("HTTP_HEADERS", ""), ":", ";")
	}
	return nil
}

func parseKVs(kvs string, ksep string, kvsep string) []string {
	var l []string
	for _, kv := range strings.Split(kvs, kvsep) {
		kv := strings.Split(kv, ksep)
		if len(kv) == 2 {
			l = append(l, strings.TrimSpace(kv[0]))
			l = append(l, strings.TrimSpace(kv[1]))
		}
	}
	return l
}

func (app *MockApp) Shutdown() error {
	if app.flowsApi != nil {
		app.flowsApi.Shutdown()
	}
	if app.server != nil {
		return app.server.Shutdown(context.Background())
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseEnv()
	if err != nil {
		return err
	}
	if app.headers != nil {
		app.client = *app.client.WithHeaders(app.headers...)
	}
	iso18626.InitNs()
	log.Info("Mock starting")
	if app.agencyType == "" {
		app.agencyType = "MOCK"
	}
	requester := &app.requester
	if requester.requestingAgencyId == "" {
		requester.requestingAgencyId = "REQ"
	}
	if requester.supplyingAgencyId == "" {
		requester.supplyingAgencyId = "SUP"
	}
	addr := app.httpPort
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	if app.flowsApi == nil {
		app.flowsApi = flows.CreateFlowsApi()
		err := app.flowsApi.ParseEnv()
		if err != nil {
			return err
		}
	}
	app.sruApi = sruapi.CreateSruApi()
	log.Info("Start HTTP serve on " + addr)
	mux := http.NewServeMux()
	iso18626Handler := iso18626Handler(app)
	mux.HandleFunc("/iso18626", iso18626Handler)
	mux.HandleFunc("/iso18626/error400", error400Handler())
	mux.HandleFunc("/iso18626/error500", error500Handler())
	reqForm := &reqform.ReqForm{
		Header:      "illmock ISO18626 submit form",
		FormPath:    "/form",
		IllPath:     "/iso18626",
		FlowsPath:   "/api/flows",
		HandlerFunc: iso18626Handler,
	}
	mux.HandleFunc(reqForm.FormPath, reqForm.HandleForm)
	mux.HandleFunc("/healthz", healthHandler())
	mux.HandleFunc("/api/flows", app.flowsApi.HttpHandler())
	mux.HandleFunc("/sru", app.sruApi.HttpHandler())

	dir, err := dirmock.NewEnv()
	if err != nil {
		return err
	}
	dir.HandlerFromMux(mux)

	app.server = &http.Server{Addr: addr, Handler: mux}
	app.flowsApi.Run()
	return app.server.ListenAndServe()
}
