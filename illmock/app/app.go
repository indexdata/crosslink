package app

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/slogwrap"
)

type Role string

const (
	Supplier  Role = "supplier"
	Requester Role = "requester"
)

type MockApp struct {
	role     Role
	httpPort string
}

var log *slog.Logger = slogwrap.SlogWrap()

func (app *MockApp) handleIso18626Request(illRequest *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626Request")
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626RequestingAgencyMessage")
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626SupplyingAgencyMessage")
}

func iso18626Handler(app *MockApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Info("[iso18626-handler] error: method not allowed", "method", r.Method, "url", r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
			log.Info("[iso18626-handler] error: content-type unsupported", "contentType", contentType, "url", r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			log.Info("[iso18626-server] error: failure reading request: ", "error", err)
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
			app.handleIso18626Request(&illMessage, w)
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

func (app *MockApp) parseConfig() error {
	app.httpPort = os.Getenv("HTTP_PORT")
	if app.httpPort == "" {
		app.httpPort = "8081"
	}
	role := os.Getenv("ILLMOCK_ROLE")
	switch strings.ToLower(role) {
	case string(Supplier):
		app.role = Supplier
	case string(Requester):
		app.role = Requester
	case "":
		app.role = Supplier
	default:
		return fmt.Errorf("bad value for ILLMOCK_ROLE")
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseConfig()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler(app))
	return http.ListenAndServe(":"+app.httpPort, mux)
}
