package adapter

import (
	"encoding/json"
	"fmt"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/go-utils/utils"
	"io"
	"net/http"
	"net/url"
)

type ApiDirectory struct {
	url    string
	client *http.Client
}

func CreateApiDirectory(client *http.Client, url string) DirectoryLookupAdapter {
	return &ApiDirectory{client: client, url: url}
}
func (a *ApiDirectory) Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error) {
	cql := "symbol any"
	for _, s := range params.Symbols {
		cql += " " + s
	}
	fullUrl := a.url + "?maximumRecords=1000&cql=" + url.QueryEscape(cql)
	log := slogwrap.SlogWrap()
	log.Info("ApiDir", "url", fullUrl)
	response, err := a.client.Get(fullUrl)
	if err != nil {
		return []DirectoryEntry{}, err
	}
	defer response.Body.Close()

	body := utils.Must(io.ReadAll(response.Body))
	if response.StatusCode != http.StatusOK {
		return []DirectoryEntry{}, fmt.Errorf("API returned non-OK status: %d, body: %s", response.StatusCode, body)
	}

	var directoryList []DirectoryEntry
	var responseList EntriesResponse
	err = json.Unmarshal(body, &responseList)
	if err != nil {
		return []DirectoryEntry{}, err
	}
	for _, d := range responseList.Items {
		var symbols []string
		if d.Symbols != nil {
			for _, s := range *d.Symbols {
				symbols = append(symbols, s.Authority+":"+s.Symbol)
			}
		}
		apiUrl := ""
		if d.Endpoints != nil && len(*d.Endpoints) > 0 {
			apiUrl = (*d.Endpoints)[0].Address
		}
		if apiUrl != "" && len(symbols) > 0 {
			entry := DirectoryEntry{
				Name:   d.Name,
				Symbol: symbols,
				Vendor: "api",
				URL:    apiUrl,
			}
			directoryList = append(directoryList, entry)
		}
	}
	return directoryList, nil
}

type EntriesResponse struct {
	Items []Entry `json:"items"`
}

type Entry struct {
	Endpoints *[]ServiceEndpoint `json:"endpoints,omitempty"`
	Name      string             `json:"name"`
	Symbols   *[]Symbol          `json:"symbols,omitempty"`
}

type Symbol struct {
	Authority string `json:"authority"`
	Symbol    string `json:"symbol"`
}

type ServiceEndpoint struct {
	Address string `json:"address"`
}
