package main

import (
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/indexdata/crosslink/broker/iso18626"
)

func handleIso18626Request(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Printf("handleIso18626Request")
}

func handleIso18626RequestingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Printf("handleIso18626RequestingAgencyMessage")
}

func handleIso18626SupplyingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Printf("handleIso18626SupplyingAgencyMessage")
}

func iso18626Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Printf("[iso18626-handler] error: method not allowed: %s %s\n", r.Method, r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
			log.Printf("[iso18626-handler] error: content-type unsupported: %s %s\n", contentType, r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println("[iso18626-server] error: failure reading request: ", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.ISO18626Message
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if illMessage.Request != nil {
			log.Printf("Got ISO 18626 Request")
			handleIso18626Request(&illMessage, w)
		} else if illMessage.RequestingAgencyMessage != nil {
			handleIso18626RequestingAgencyMessage(&illMessage, w)
		} else if illMessage.SupplyingAgencyMessage != nil {
			handleIso18626SupplyingAgencyMessage(&illMessage, w)
		} else {
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler())

	var httpPort, _ = os.LookupEnv("HTTP_PORT")
	if httpPort == "" {
		httpPort = "8081"
	}
	http.ListenAndServe(":"+httpPort, mux)
}
