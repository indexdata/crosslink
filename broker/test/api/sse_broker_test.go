package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSseEndpointSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go sendMessages(ctx) //Send messages every 5 milliseconds
	done := make(chan bool)
	inErr := make(chan error)
	go func() {
		resp, err := http.Get(getLocalhostWithPort() + "/sse/events?side=borrowing&symbol=ISIL:REQ")
		if err != nil {
			inErr <- err
			return
		}
		defer resp.Body.Close()

		// Verify headers
		if contentType := resp.Header.Get("Content-Type"); contentType != "text/event-stream" {
			inErr <- errors.New("Expected text/event-stream, got " + contentType)
		}

		results := make(chan string, 1)
		errChan := make(chan error, 1)
		go func() {
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					results <- strings.TrimPrefix(line, "data: ")
					return // Exit after receiving the first event for this test
				}
			}
			if err := scanner.Err(); err != nil {
				errChan <- err
			}
		}()

		select {
		case data := <-results:
			if data == "" {
				t.Error("Received empty data from SSE")
			}
			t.Logf("Successfully received: %s", data)
			assert.True(t, strings.Contains(data, "{\"event\":\"message-requester\",\"data\":{\"supplyingAgencyMessage\":"))
		case err := <-errChan:
			inErr <- err
		}
		cancel()
		done <- true
	}()

	select {
	case err := <-inErr:
		assert.NoError(t, err)
	default:
		// No errors
	}

	select {
	case <-done:
		// Test finished successfully
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}
}

func sendMessages(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Shutting down sendMessages...")
			return
		case t := <-ticker.C:
			executeTask(t)
		}
	}
}

func TestSseEndpointNoSide(t *testing.T) {
	resp, err := http.Get(getLocalhostWithPort() + "/sse/events?symbol=ISIL:REQ")
	assert.NoError(t, err)
	bodyBytes, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, "query parameter 'side' and 'symbol' must be specified\n", string(bodyBytes))
}

func TestSseEndpointNoSymbol(t *testing.T) {
	resp, err := http.Get(getLocalhostWithPort() + "/sse/events?side=borrowing")
	assert.NoError(t, err)
	bodyBytes, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	assert.Equal(t, "query parameter 'side' and 'symbol' must be specified\n", string(bodyBytes))
}

func executeTask(t time.Time) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	sseBroker.IncomingIsoMessage(ctx, events.Event{EventName: events.EventNameMessageRequester,
		ResultData: events.EventResult{
			CommonEventData: events.CommonEventData{
				OutgoingMessage: &iso18626.ISO18626Message{
					SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
						Header: iso18626.Header{
							RequestingAgencyId: iso18626.TypeAgencyId{
								AgencyIdType: iso18626.TypeSchemeValuePair{
									Text: "ISIL",
								},
								AgencyIdValue: "REQ",
							},
						},
						MessageInfo: iso18626.MessageInfo{
							Note: t.String(),
						},
					},
				},
			},
		}})
}
