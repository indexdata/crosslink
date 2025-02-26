package httpclient

import (
	"bytes"
	"encoding/xml"
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

type HttpError struct {
	StatusCode int
	message    string
}

func (e *HttpError) Error() string {
	return e.message
}

func httpInvoke(client *http.Client, method string, contentTypes []string, url string, reader io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Add(ContentType, contentTypes[0])
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &HttpError{resp.StatusCode, fmt.Sprintf("HTTP error: %d", resp.StatusCode)}
	}
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	contentType := strings.ToLower(resp.Header.Get(ContentType))
	for _, ctype := range contentTypes {
		if strings.HasPrefix(contentType, ctype) {
			return buf, nil
		}
	}
	return nil, fmt.Errorf("header Content-Type must be one of: %s", strings.Join(contentTypes, ", "))
}

func GetXml(client *http.Client, url string, res any) error {
	return requestXml(client, http.MethodGet, url, res)
}

func PostXml(client *http.Client, url string, req any, res any) error {
	return requestResponseXml(client, http.MethodPost, url, req, res)
}

// leaving these private until we are happy with them

func requestXml(client *http.Client, method string, url string, res any) error {
	return request(client, method, []string{ContentTypeApplicationXml, ContentTypeTextXml}, url, res, xml.Unmarshal)
}

func requestResponseXml(client *http.Client, method string, url string, req any, res any) error {
	return requestResponse(client, method, []string{ContentTypeApplicationXml, ContentTypeTextXml}, url, req, res, xml.Marshal, xml.Unmarshal)
}

func request(client *http.Client, method string, contentTypes []string, url string, res any, unmarshal func([]byte, any) error) error {
	resbuf, err := httpInvoke(client, method, contentTypes, url, nil)
	if err != nil {
		return err
	}
	return unmarshal(resbuf, res)
}

func requestResponse(client *http.Client, method string, contentTypes []string, url string, req any, res any, marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) error {
	buf, err := marshal(req)
	if err != nil {
		return fmt.Errorf("marshal failed: %v", err)
	}
	resbuf, err := httpInvoke(client, method, contentTypes, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	return unmarshal(resbuf, res)
}
