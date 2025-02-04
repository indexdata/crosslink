package httpclient

import (
	"bytes"
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

func clientDo(client *http.Client, method string, url string, reader io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Add(ContentType, ContentTypeApplicationXml)
	return client.Do(req)
}

func SendReceiveXml(client *http.Client, url string, buf []byte) ([]byte, error) {
	resp, err := clientDo(client, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP POST error: %d", resp.StatusCode)
	}
	buf, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	contentType := resp.Header.Get(ContentType)
	if !strings.HasPrefix(contentType, ContentTypeApplicationXml) && !strings.HasPrefix(contentType, ContentTypeTextXml) {
		return nil, fmt.Errorf("only application/xml or text/xml accepted")
	}
	return buf, nil
}
