package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

type sendRawEmailResponse struct {
	MessageID string `json:"MessageId"`
}

func main() {
	mux := http.NewServeMux()

	// AWS Query protocol endpoint (SES v1 style path "/")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		log.Printf("mock-ses request: method=%s path=%s query=%s", r.Method, r.URL.Path, r.URL.RawQuery)
		log.Printf("headers: x-amz-target=%q content-type=%q", r.Header.Get("X-Amz-Target"), r.Header.Get("Content-Type"))
		log.Printf("body: %s", string(body))

		// Return JSON that AWS SDK SES client can parse.
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sendRawEmailResponse{
			MessageID: "mock-" + time.Now().Format("20060102150405"),
		})
	})

	addr := ":18080"
	log.Printf("mock-ses listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
