package reqform

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
)

//go:embed reqform.html
var Html []byte

//go:embed example.xml
var Example []byte

func init() {
	Html = bytes.Replace(Html, []byte("{{example}}"), []byte(html.EscapeString(string(Example))), 1)
}

type ReqForm struct {
	Header      string
	FormPath    string
	IllPath     string
	FlowsPath   string
	HandlerFunc http.HandlerFunc
}

func (rf *ReqForm) HandleForm(w http.ResponseWriter, r *http.Request) {
	Html = bytes.Replace(Html, []byte("{{header}}"), []byte(rf.Header), 2)
	Html = bytes.Replace(Html, []byte("{{path}}"), []byte(rf.FormPath), 1)
	if r.Method == http.MethodGet {
		rf.writeHTML(w, "post a message to see the response", "")
		return
	}
	if r.Method != http.MethodPost {
		log.Println("[req-form] ERROR ", http.StatusMethodNotAllowed)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		log.Println("[req-form] ERROR", http.StatusUnsupportedMediaType)
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	err := r.ParseForm()
	if err != nil {
		log.Println("[req-form] ERROR parsing form", err)
		http.Error(w, "error parsing form", http.StatusBadRequest)
		return
	}
	message := r.Form.Get("message")
	req, err := http.NewRequest(http.MethodPost, rf.IllPath, strings.NewReader(message))
	if err != nil {
		log.Println("[req-form] ERROR creating ISO18626 request", err)
		http.Error(w, "error creating ISO18626 request", http.StatusBadRequest)
		return
	}
	req.Header.Set("Content-Type", "application/xml")
	res := NewWrappedResponseWriter()
	rf.HandlerFunc(res, req)
	statusCode := res.status
	resBody := res.buf.Bytes()
	requestId := res.Header().Get("X-Request-ID")
	if statusCode != http.StatusOK {
		log.Println("[req-form] ERROR failure handling message:", statusCode, resBody)
		rf.writeHTML(w, fmt.Sprintf("%d\n%s", statusCode, resBody), requestId)
		return
	}
	rf.writeHTML(w, string(resBody), requestId)
}

func (rf *ReqForm) writeHTML(w http.ResponseWriter, response string, requestId string) {
	w.Header().Add("Content-Type", "text/html")
	flowsLink := url.URL{Path: rf.FlowsPath}
	query := flowsLink.Query()
	if requestId != "" {
		query.Set("id", requestId)
	}
	flowsLink.RawQuery = query.Encode()
	out := bytes.Replace(Html, []byte("{{flowsLink}}"), []byte(flowsLink.String()), 1)
	_, err := w.Write(bytes.Replace(out, []byte("{{response}}"), []byte(html.EscapeString(response)), 1))
	if err != nil {
		log.Println("[req-form] ERROR writing response", err)
	}
}

type WrappedResponseWriter struct {
	buf    *bytes.Buffer
	status int
	header http.Header
}

func NewWrappedResponseWriter() *WrappedResponseWriter {
	return &WrappedResponseWriter{
		buf:    new(bytes.Buffer),
		header: make(http.Header),
	}
}

func (brw *WrappedResponseWriter) Header() http.Header {
	return brw.header
}

func (brw *WrappedResponseWriter) Write(data []byte) (int, error) {
	return brw.buf.Write(data)
}

func (brw *WrappedResponseWriter) WriteHeader(statusCode int) {
	brw.status = statusCode
}
