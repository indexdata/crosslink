// Package ncipclientv1 exchanges native NCIP 1.0 messages over HTTP.
package ncipclientv1

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/httpclient"
	ncip "github.com/indexdata/crosslink/ncipv1"
)

// LogFunc receives redacted messages. Install it before using the client concurrently.
type LogFunc func(map[string]any, map[string]any, error)

// Client exchanges v1 types without translating to another protocol version.
type Client struct {
	client  *http.Client
	address string
	logFunc LogFunc
}

// New creates an HTTP client. A nil transport uses a 30 second timeout.
// Requests must include their initiation header and identifier agencies.
func New(client *http.Client, address string) *Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{client: client, address: address}
}

// SetLogFunc installs the operation logger before concurrent use.
func (c *Client) SetLogFunc(f LogFunc) { c.logFunc = f }

// Error retains the complete native v1 problem, including nested error elements.
type Error struct {
	Operation string
	Problem   ncip.Problem
}

func (e *Error) Error() string {
	kinds := []string{}
	for _, p := range e.Problem.MessagingError {
		kinds = append(kinds, p.MessagingErrorType.Value)
	}
	for _, p := range e.Problem.ProcessingError {
		kinds = append(kinds, p.ProcessingErrorType.Value)
	}
	return e.Operation + ": NCIP v1 problem: " + strings.Join(kinds, ", ")
}

// exchange removes HTTP response bodies before errors reach callers or log callbacks.
func (c *Client) exchange(outgoing, incoming *ncip.NCIPMessage) error {
	err := httpclient.NewClient().RequestResponse(c.client, http.MethodPost, []string{"application/xml", "text/xml"}, c.address, outgoing, incoming, xml.Marshal, decode)
	var httpErr *httpclient.HttpError
	if errors.As(err, &httpErr) {
		return &httpclient.HttpError{StatusCode: httpErr.StatusCode}
	}
	return err
}

// LookupUser performs the NCIP 1.0 LookupUser service.
func (c *Client) LookupUser(arg ncip.LookupUser) (response *ncip.LookupUserResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, LookupUser: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 LookupUser: %w", err)
	}
	response = incoming.LookupUserResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing LookupUserResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "LookupUser", Problem: *response.Problem}
	}
	return response, nil
}

// AcceptItem performs the NCIP 1.0 AcceptItem service.
func (c *Client) AcceptItem(arg ncip.AcceptItem) (response *ncip.AcceptItemResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, AcceptItem: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 AcceptItem: %w", err)
	}
	response = incoming.AcceptItemResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing AcceptItemResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "AcceptItem", Problem: *response.Problem}
	}
	return response, nil
}

// RequestItem performs the NCIP 1.0 RequestItem service.
func (c *Client) RequestItem(arg ncip.RequestItem) (response *ncip.RequestItemResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, RequestItem: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 RequestItem: %w", err)
	}
	response = incoming.RequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing RequestItemResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "RequestItem", Problem: *response.Problem}
	}
	return response, nil
}

// CancelRequestItem performs the NCIP 1.0 CancelRequestItem service.
func (c *Client) CancelRequestItem(arg ncip.CancelRequestItem) (response *ncip.CancelRequestItemResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, CancelRequestItem: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 CancelRequestItem: %w", err)
	}
	response = incoming.CancelRequestItemResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing CancelRequestItemResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "CancelRequestItem", Problem: *response.Problem}
	}
	return response, nil
}

// CheckInItem performs the NCIP 1.0 CheckInItem service.
func (c *Client) CheckInItem(arg ncip.CheckInItem) (response *ncip.CheckInItemResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, CheckInItem: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 CheckInItem: %w", err)
	}
	response = incoming.CheckInItemResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing CheckInItemResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "CheckInItem", Problem: *response.Problem}
	}
	return response, nil
}

// CheckOutItem performs the NCIP 1.0 CheckOutItem service.
func (c *Client) CheckOutItem(arg ncip.CheckOutItem) (response *ncip.CheckOutItemResponse, err error) {
	outgoing := &ncip.NCIPMessage{Version: ncip.NCIP_V1_0_DTD, CheckOutItem: &arg}
	incoming := &ncip.NCIPMessage{}
	defer func() { c.log(outgoing, incoming, err) }()
	err = c.exchange(outgoing, incoming)
	if err != nil {
		return nil, fmt.Errorf("NCIP v1 CheckOutItem: %w", err)
	}
	response = incoming.CheckOutItemResponse
	if response == nil {
		return nil, fmt.Errorf("NCIP v1: missing CheckOutItemResponse")
	}
	if response.Problem != nil {
		return response, &Error{Operation: "CheckOutItem", Problem: *response.Problem}
	}
	return response, nil
}

// Bound nesting before unmarshalling recursive error elements; reject foreign
// namespaces, multiple services and trailing documents instead of accepting
// encoding/xml's otherwise permissive envelope handling.
func decode(data []byte, value any) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, roots, services := 0, 0, 0
	fields := make(map[string]int)
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 64 || t.Name.Space != "" {
				return fmt.Errorf("invalid NCIP v1 nesting or namespace")
			}
			if depth == 1 {
				roots++
				if roots != 1 || t.Name.Local != "NCIPMessage" {
					return fmt.Errorf("invalid NCIP v1 envelope")
				}
				for _, a := range t.Attr {
					if a.Name.Local == "version" && a.Value != "" && a.Value != ncip.NCIP_V1_0_DTD {
						return fmt.Errorf("unsupported NCIP v1 version %q", a.Value)
					}
				}
			}
			if depth == 2 {
				services++
			}
			if depth == 3 {
				fields[t.Name.Local]++
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth < 2 && strings.TrimSpace(string(t)) != "" {
				return fmt.Errorf("unexpected NCIP v1 envelope text")
			}
		}
	}
	if roots != 1 || services != 1 {
		return fmt.Errorf("NCIP v1 requires one response service")
	}
	if err := xml.Unmarshal(data, value); err != nil {
		return err
	}
	message, ok := value.(*ncip.NCIPMessage)
	if !ok {
		return fmt.Errorf("NCIP v1 response target must be NCIPMessage")
	}
	return validateResponse(message, fields)
}

func (c *Client) log(out, in *ncip.NCIPMessage, err error) {
	if c.logFunc == nil {
		return
	}
	outgoing, e := common.StructToMap(out)
	if err == nil {
		err = e
	}
	incoming, e := common.StructToMap(in)
	if err == nil {
		err = e
	}
	redact(outgoing)
	redact(incoming)
	c.logFunc(outgoing, incoming, err)
}
func redact(value any) {
	switch v := value.(type) {
	case map[string]any:
		for k, f := range v {
			switch k {
			case "FromAgencyAuthentication", "FromSystemAuthentication", "AuthenticationInputData", "NameInformation", "UserAddressInformation", "MessagingErrorValue", "ProcessingErrorValue":
				v[k] = "***"
			default:
				redact(f)
			}
		}
	case []any:
		for _, f := range v {
			redact(f)
		}
	}
}
