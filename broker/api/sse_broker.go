package api

import (
	"encoding/json"
	"fmt"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"net/http"
	"sync"
)

type SseBroker struct {
	input   chan SseMessage
	clients map[string]map[chan string]bool
	mu      sync.Mutex
	ctx     common.ExtendedContext
	tenant  common.Tenant
}

func NewSseBroker(ctx common.ExtendedContext, tenant common.Tenant) (broker *SseBroker) {
	broker = &SseBroker{
		input:   make(chan SseMessage),
		clients: make(map[string]map[chan string]bool),
		ctx:     ctx,
		tenant:  tenant,
	}

	// Start the single broadcaster goroutine
	go broker.run()
	return broker
}
func (b *SseBroker) run() {
	b.ctx.Logger().Debug("SseBroker running...")
	for {
		// Wait for an event from the application logic
		event := <-b.input

		b.mu.Lock()
		for clientChannel := range b.clients[event.receiver] {
			select {
			case clientChannel <- event.message:
				// Successfully sent
			default:
				// Client is slow or disconnected, remove them to prevent memory leak
				b.removeClient(event.receiver, clientChannel)
			}
		}
		b.mu.Unlock()
	}
}

func (b *SseBroker) removeClient(receiver string, clientChannel chan string) {
	clients := b.clients[receiver]
	if clients != nil {
		delete(clients, clientChannel)
		if len(clients) == 0 {
			delete(b.clients, receiver)
		}
	}
	close(clientChannel)
	b.ctx.Logger().Debug("Client channel closed and removed.")
}

// ServeHTTP implements the http.Handler interface for the SSE endpoint.
func (b *SseBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientChannel := make(chan string, 10)
	tenant := r.Header.Get("X-Okapi-Tenant")
	var symbol string
	if b.tenant.IsSpecified() && tenant != "" {
		symbol = b.tenant.GetSymbol(tenant)
	} else {
		symbol = r.URL.Query().Get("symbol")
	}

	side := r.URL.Query().Get("side")
	if side == "" || symbol == "" {
		http.Error(w, "query parameter 'side' and 'symbol' must be specified", http.StatusBadRequest)
		return
	}
	if side != string(prservice.SideBorrowing) && side != string(prservice.SideLending) {
		http.Error(w, fmt.Sprintf("query parameter 'side' must be %s or %s", prservice.SideBorrowing, prservice.SideLending), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	receiver := side + symbol
	clients := b.clients[receiver]
	if clients != nil {
		clients[clientChannel] = true
	} else {
		b.clients[receiver] = map[chan string]bool{clientChannel: true}
	}
	b.mu.Unlock()
	b.ctx.Logger().Debug(fmt.Sprintf("new client registered: %s", receiver))

	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.removeClient(receiver, clientChannel)
	}()

	// Set SSE Headers and get Flusher
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Context for connection status check
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client connection closed
			return

		case event := <-clientChannel:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (b *SseBroker) SubmitMessageToChannels(message SseMessage) {
	b.input <- message
}

type SseMessage struct {
	receiver string
	message  string
}

type SseIsoMessageEvent struct {
	Event events.EventName         `json:"event,omitempty"`
	Data  iso18626.ISO18626Message `json:"data,omitempty"`
}

func (b *SseBroker) IncomingIsoMessage(ctx common.ExtendedContext, event events.Event) {
	if event.ResultData.OutgoingMessage != nil {
		sseEvent := SseIsoMessageEvent{
			Data:  *event.ResultData.OutgoingMessage,
			Event: event.EventName,
		}
		symbol := ""
		var side pr_db.PatronRequestSide
		if event.ResultData.OutgoingMessage.RequestingAgencyMessage != nil {
			side = prservice.SideLending
			symbol = getSymbol(event.ResultData.OutgoingMessage.RequestingAgencyMessage.Header.SupplyingAgencyId)
		} else if event.ResultData.OutgoingMessage.SupplyingAgencyMessage != nil {
			side = prservice.SideBorrowing
			symbol = getSymbol(event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.RequestingAgencyId)
		} else {
			return
		}
		updateMessageBytes, err := json.Marshal(sseEvent)
		if err != nil {
			ctx.Logger().Error("failed to parse event data", "error", err)
			return
		}
		b.SubmitMessageToChannels(SseMessage{receiver: string(side) + symbol, message: string(updateMessageBytes)})
	}
}

func getSymbol(agencyId iso18626.TypeAgencyId) string {
	return agencyId.AgencyIdType.Text + ":" + agencyId.AgencyIdValue
}
