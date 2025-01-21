package http18626

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/slogwrap"
)

var log *slog.Logger = slogwrap.SlogWrap()

func SendReceiveDefault(url string, msg *iso18626.Iso18626MessageNS) (*iso18626.ISO18626Message, error) {
	return SendReceive(http.DefaultClient, url, msg)
}

func SendReceive(client *http.Client, url string, msg *iso18626.Iso18626MessageNS) (*iso18626.ISO18626Message, error) {
	buf, err := xml.Marshal(msg)
	if err != nil {
		return nil, err
	}
	log.Info("send XML", "url", url, "xml", buf)
	req, err := http.NewRequest("POST", url+"/iso18626", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/xml")
	resp, err := client.Do(req)
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
	log.Info("recv XML", "xml", buf)
	var response iso18626.ISO18626Message
	err = xml.Unmarshal(buf, &response)
	if err != nil {
		return nil, err
	}
	return &response, nil
}
