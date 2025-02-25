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

func Request(client *http.Client, method string, contentTypes []string, url string, reader io.Reader) ([]byte, error) {
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

func getXmlResponse(client *http.Client, method string, url string, reader io.Reader, res any) error {
	resbuf, err := Request(client, method, []string{ContentTypeApplicationXml, ContentTypeTextXml}, url, reader)
	if err != nil {
		return err
	}
	return xml.Unmarshal(resbuf, res)
}

func GetXml(client *http.Client, url string, res any) error {
	return getXmlResponse(client, http.MethodGet, url, nil, res)
}

func PostXml(client *http.Client, url string, req any, res any) error {
	buf, err := xml.Marshal(req)
	if err != nil || buf == nil {
		return fmt.Errorf("xml.Marshal failed")
	}
	return getXmlResponse(client, http.MethodPost, url, bytes.NewReader(buf), res)
}
