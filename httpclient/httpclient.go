package httpclient

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	ContentTypeTextXml        string = "text/xml"
	ContentTypeApplicationXml string = "application/xml"
	ContentType               string = "Content-Type"
)

const DefaultMaxResponseSize int64 = 1024 * 1024 * 10 // 10MB

type HttpError struct {
	StatusCode int
	Body       []byte
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("HTTP error %d: %s", e.StatusCode, e.Body)
}

type HttpClient struct {
	Headers         http.Header
	MaxResponseSize int64
}

func NewClient() *HttpClient {
	return &HttpClient{Headers: http.Header{}, MaxResponseSize: DefaultMaxResponseSize}
}

func (c *HttpClient) WithMaxSize(maxResponseSize int64) *HttpClient {
	c.MaxResponseSize = maxResponseSize
	return c
}

func (c *HttpClient) WithHeaders(headers ...string) *HttpClient {
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "" {
			continue
		}
		c.Headers.Add(headers[i], headers[i+1])
	}
	return c
}

func (c *HttpClient) httpInvoke(client *http.Client, method string, contentTypes []string, url string, reader io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	if c.Headers != nil {
		req.Header = c.Headers.Clone()
	}
	req.Header.Set(ContentType, contentTypes[0])
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		dErr := resp.Body.Close()
		if dErr != nil {
			fmt.Printf("failed to close body: %v", dErr)
		}
	}()
	buf, err := c.readResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &HttpError{resp.StatusCode, buf}
	}
	contentType := strings.ToLower(resp.Header.Get(ContentType))
	for _, ctype := range contentTypes {
		if strings.HasPrefix(contentType, ctype) {
			return buf, nil
		}
	}
	return nil, fmt.Errorf("header Content-Type must be one of: %s", strings.Join(contentTypes, ", "))
}

func (c *HttpClient) readResponse(body io.Reader) ([]byte, error) {
	if c.MaxResponseSize > 0 {
		body = NewLimitErrorReader(body, c.MaxResponseSize)
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

type LimitErrorReader struct {
	reader *io.LimitedReader
}

func NewLimitErrorReader(r io.Reader, limit int64) *LimitErrorReader {
	return &LimitErrorReader{
		reader: &io.LimitedReader{R: r, N: limit},
	}
}

func (ler *LimitErrorReader) Read(p []byte) (int, error) {
	if ler.reader.N <= 0 {
		return 0, errors.New("response body too large")
	}
	return ler.reader.Read(p)
}

func (c *HttpClient) GetXml(client *http.Client, url string, res any) error {
	return c.requestXml(client, http.MethodGet, url, res)
}

func (c *HttpClient) PostXml(client *http.Client, url string, req any, res any) error {
	return c.requestResponseXml(client, http.MethodPost, url, req, res)
}

// leaving these private until we are happy with them

func (c *HttpClient) requestXml(client *http.Client, method string, url string, res any) error {
	return c.request(client, method, []string{ContentTypeApplicationXml, ContentTypeTextXml}, url, res, xml.Unmarshal)
}

func (c *HttpClient) requestResponseXml(client *http.Client, method string, url string, req any, res any) error {
	return c.RequestResponse(client, method, []string{ContentTypeApplicationXml, ContentTypeTextXml}, url, req, res, xml.Marshal, xml.Unmarshal)
}

func (c *HttpClient) request(client *http.Client, method string, contentTypes []string, url string, res any, unmarshal func([]byte, any) error) error {
	resbuf, err := c.httpInvoke(client, method, contentTypes, url, nil)
	if err != nil {
		return err
	}
	return unmarshal(resbuf, res)
}

func (c *HttpClient) RequestResponse(client *http.Client, method string, contentTypes []string, url string, req any, res any, marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) error {
	buf, err := marshal(req)
	if err != nil {
		return fmt.Errorf("marshal failed: %v", err)
	}
	if buf == nil {
		return fmt.Errorf("marshal returned nil")
	}
	resbuf, err := c.httpInvoke(client, method, contentTypes, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	return unmarshal(resbuf, res)
}
