package reqform

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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
	Path        string
	HandlerFunc http.HandlerFunc
}

func (rf *ReqForm) HandleForm(w http.ResponseWriter, r *http.Request) {
	Html = bytes.Replace(Html, []byte("{{header}}"), []byte(rf.Header), 2)
	Html = bytes.Replace(Html, []byte("{{path}}"), []byte(rf.Path), 1)
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		log.Println("[req-form] ERROR ", http.StatusMethodNotAllowed)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			r.ParseForm()
			message := r.Form.Get("message")
			req := httptest.NewRequest(http.MethodPost, "/iso18626", strings.NewReader(message))
			req.Header.Set("Content-Type", "application/xml")
			resRec := httptest.NewRecorder()
			rf.HandlerFunc(resRec, req)
			res := resRec.Result()
			resBody, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK {
				log.Println("[req-form] ERROR failure handling message: ", res.Status, resBody)
				rf.writeHTML(w, fmt.Sprintf("%s\n%s", res.Status, resBody))
				return
			}
			rf.writeHTML(w, string(resBody))
		} else {
			log.Println("[req-form] ERROR ", http.StatusUnsupportedMediaType)
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		}
	} else {
		rf.writeHTML(w, "post a message to see the response")
	}
}

func (rf *ReqForm) writeHTML(w http.ResponseWriter, response string) {
	w.Header().Add("Content-Type", "text/html")
	w.Write(bytes.Replace(Html, []byte("{{response}}"), []byte(html.EscapeString(response)), 1))
}
