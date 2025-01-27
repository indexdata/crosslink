package httpclient

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/go-utils/utils"
)

const (
	ContentTypeTextXml        string = "text/xml"
	ContentTypeApplicationXml string = "application/xml"
	ContentType               string = "Content-Type"
)

var log *slog.Logger = slogwrap.SlogWrap()

func SendReceiveDefault(url string, msg *iso18626.Iso18626MessageNS) (*iso18626.ISO18626Message, error) {
	return SendReceive(http.DefaultClient, url, msg)
}

func clientDo(client *http.Client, method string, url string, reader io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Add(ContentType, ContentTypeApplicationXml)
	return client.Do(req)
}

func SendReceive(client *http.Client, url string, msg *iso18626.Iso18626MessageNS) (*iso18626.ISO18626Message, error) {
	buf := utils.Must(xml.MarshalIndent(msg, "  ", "  "))
	if buf == nil {
		return nil, fmt.Errorf("marshal failed")
	}
	lead := fmt.Sprintf("send XML\n%s", buf)
	log.Info(lead)
	resp, err := clientDo(client, http.MethodPost, url+"/iso18626", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP POST error: %d", resp.StatusCode)
	}
	contentType := resp.Header.Get(ContentType)
	log.Info("recv", "Content-Type", contentType)
	if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
		return nil, fmt.Errorf("only application/xml or text/xml accepted")
	}
	var response iso18626.ISO18626Message
	err = xml.Unmarshal(buf, &response)
	if err != nil {
		return nil, err
	}
	buf1, _ := xml.MarshalIndent(&response, "  ", "  ")
	if buf1 != nil {
		lead = fmt.Sprintf("recv XML\n%s", buf1)
		log.Info(lead)
	}
	return &response, nil
}
