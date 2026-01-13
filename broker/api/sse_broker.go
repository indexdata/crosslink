package api

import (
	"encoding/json"
	"fmt"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/iso18626"
	"net/http"
	"sync"
)

type SseBroker struct {
	input   chan string
	clients map[chan string]bool
	mu      sync.Mutex
	ctx     common.ExtendedContext
}

func NewSseBroker(ctx common.ExtendedContext) (broker *SseBroker) {
	broker = &SseBroker{
		input:   make(chan string),
		clients: make(map[chan string]bool),
		ctx:     ctx,
	}

	// Start the single broadcaster goroutine
	go broker.run()
	return broker
}
func (b *SseBroker) run() {
	b.ctx.Logger().Info("SeeBroker running...")
	for {
		// Wait for an event from the application logic
		event := <-b.input

		b.mu.Lock()
		for clientChannel := range b.clients {
			select {
			case clientChannel <- event:
				// Successfully sent
			default:
				// Client is slow or disconnected, remove them to prevent memory leak
				b.removeClient(clientChannel)
			}
		}
		b.mu.Unlock()
	}
}

func (b *SseBroker) removeClient(clientChannel chan string) {
	b.mu.Lock()
	delete(b.clients, clientChannel)
	close(clientChannel)
	b.mu.Unlock()
	b.ctx.Logger().Info("Client channel closed and removed.")
}

// ServeHTTP implements the http.Handler interface for the SSE endpoint.
func (b *SseBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientChannel := make(chan string, 10)

	b.mu.Lock()
	b.clients[clientChannel] = true
	b.mu.Unlock()
	b.ctx.Logger().Info("Client registered. Total clients: " + string(rune(len(b.clients))))

	defer b.removeClient(clientChannel)

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

func (b *SseBroker) SubmitMessageToChannels(message string) {
	b.input <- message
}

type SeeIsoMessageEvent struct {
	Event events.EventName         `json:"event,omitempty"`
	Data  iso18626.ISO18626Message `json:"data,omitempty"`
}

func (b *SseBroker) IncomingIsoMessage(ctx common.ExtendedContext, event events.Event) {
	if event.EventData.IncomingMessage != nil {
		sseEvent := SeeIsoMessageEvent{
			Event: event.EventName,
			Data:  *event.EventData.IncomingMessage,
		}
		updateMessageBytes, err := json.Marshal(sseEvent)
		if err != nil {
			ctx.Logger().Error("failed to parse event data", "error", err)
			return
		}
		b.SubmitMessageToChannels(string(updateMessageBytes))
	}
}
