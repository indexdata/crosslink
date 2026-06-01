package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// main starts a mock SES Server that implements the AWS Query Protocol
// for the SendRawEmail endpoint, used for testing email functionality.
func main() {
	mux := http.NewServeMux()

	// AWS Query protocol endpoint (SES v1 style path "/")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if err := r.Body.Close(); err != nil {
			log.Printf("mock-ses: failed to close request body: %v", err)
		}

		log.Printf("mock-ses request: method=%s path=%s query=%s", r.Method, r.URL.Path, r.URL.RawQuery)
		log.Printf("headers: x-amz-target=%q content-type=%q", r.Header.Get("X-Amz-Target"), r.Header.Get("Content-Type"))
		log.Printf("body: %s", string(body))

		// Return XML that AWS SDK SES client expects for Query protocol.
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)

		msgID := "mock-" + time.Now().Format("20060102150405")
		xmlResp := fmt.Sprintf(`<SendRawEmailResponse xmlns="http://ses.amazonaws.com/doc/2010-12-01/">
  <SendRawEmailResult>
    <MessageId>%s</MessageId>
  </SendRawEmailResult>
  <ResponseMetadata>
    <RequestId>mock-request-id</RequestId>
  </ResponseMetadata>
</SendRawEmailResponse>`, msgID)
		_, _ = w.Write([]byte(xmlResp))
	})

	addr := ":18080"
	log.Printf("mock-ses listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
