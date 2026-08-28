package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/iso18626"
)

const (
	sseHeartbeatInterval = 15 * time.Second
	sseRetryMilliseconds = 3000
)

type SseBroker struct {
	input          chan SseMessage
	clients        map[string]map[chan string]bool
	mu             sync.Mutex
	ctx            common.ExtendedContext
	tenantResolver *tenant.TenantResolver
	heartbeat      time.Duration
}

func NewSseBroker(ctx common.ExtendedContext, tenantResolver *tenant.TenantResolver) (broker *SseBroker) {
	broker = &SseBroker{
		input:          make(chan SseMessage),
		clients:        make(map[string]map[chan string]bool),
		ctx:            ctx,
		tenantResolver: tenantResolver,
		heartbeat:      sseHeartbeatInterval,
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
				if b.removeClientLocked(event.receiver, clientChannel) {
					b.ctx.Logger().Warn("SSE client evicted because its message buffer is full", "receiver", event.receiver)
				}
			}
		}
		b.mu.Unlock()
	}
}

// removeClientLocked removes and closes a client channel if it is still
// registered. The caller must hold b.mu.
func (b *SseBroker) removeClientLocked(receiver string, clientChannel chan string) bool {
	clients := b.clients[receiver]
	if clients == nil {
		return false
	}
	if _, registered := clients[clientChannel]; !registered {
		return false
	}
	delete(clients, clientChannel)
	if len(clients) == 0 {
		delete(b.clients, receiver)
	}
	close(clientChannel)
	return true
}

// ServeHTTP implements the http.Handler interface for the SSE endpoint.
func (b *SseBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logParams := map[string]string{"method": "ServeHTTP"}
	ectx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	suppliedSymbol := r.URL.Query().Get("symbol")
	resolvedTenant, err := b.tenantResolver.Resolve(ectx, r, &suppliedSymbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	symbol, err := resolvedTenant.GetRequestSymbol()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if symbol == "" {
		http.Error(w, "symbol must be specified", http.StatusBadRequest)
		return
	}
	side := r.URL.Query().Get("side")
	if side == "" {
		http.Error(w, "query parameter 'side' must be specified", http.StatusBadRequest)
		return
	}
	if side != string(prservice.SideBorrowing) && side != string(prservice.SideLending) {
		http.Error(w, fmt.Sprintf("query parameter 'side' must be %s or %s", prservice.SideBorrowing, prservice.SideLending), http.StatusBadRequest)
		return
	}
	clientChannel := make(chan string, 10)
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
		if b.removeClientLocked(receiver, clientChannel) {
			b.ctx.Logger().Debug("SSE client disconnected", "receiver", receiver)
		}
	}()

	// Set SSE headers. Okapi owns CORS for proxied requests and supplies the
	// credential-aware origin headers. The wildcard is retained only for direct
	// access to the broker.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if !tenant.IsOkapiRequest(r) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		b.ctx.Logger().Warn("failed to clear SSE write deadline", "receiver", receiver, "error", err)
	}

	// Sending retry also commits and flushes the headers, so the client can
	// distinguish an established idle stream from a request that never arrived.
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", sseRetryMilliseconds); err != nil {
		b.ctx.Logger().Debug("failed to establish SSE stream", "receiver", receiver, "error", err)
		return
	}
	if err := controller.Flush(); err != nil {
		b.ctx.Logger().Debug("failed to flush SSE stream", "receiver", receiver, "error", err)
		return
	}

	// Context for connection status check
	ctx := r.Context()
	heartbeat := time.NewTicker(b.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			// Client connection closed
			return

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				b.ctx.Logger().Debug("failed to write SSE heartbeat", "receiver", receiver, "error", err)
				return
			}
			if err := controller.Flush(); err != nil {
				b.ctx.Logger().Debug("failed to flush SSE heartbeat", "receiver", receiver, "error", err)
				return
			}

		case event, ok := <-clientChannel:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				b.ctx.Logger().Debug("failed to write SSE event", "receiver", receiver, "error", err)
				return
			}
			if err := controller.Flush(); err != nil {
				b.ctx.Logger().Debug("failed to flush SSE event", "receiver", receiver, "error", err)
				return
			}
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
