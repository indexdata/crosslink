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
		if listMap, ok := d["symbols"].([]any); ok && len(listMap) > 0 {
			for _, s := range listMap {
				if itemMap, castOk := s.(map[string]any); castOk {
					auth, authOk := itemMap["authority"].(string)
					sym, symOk := itemMap["symbol"].(string)
					if authOk && symOk {
						symbols = append(symbols, auth+":"+sym)
					}
				}
			}
		}
		apiUrl := ""
		if listMap, ok := d["endpoints"].([]any); ok && len(listMap) > 0 {
			apiUrl = listMap[0].(map[string]any)["address"].(string)
		}
		if apiUrl != "" && len(symbols) > 0 {
			entry := DirectoryEntry{
				Name:       d["name"].(string),
				Symbol:     symbols,
				Vendor:     "api",
				URL:        apiUrl,
				CustomData: d,
			}
			directoryList = append(directoryList, entry)
		}
	}
	return directoryList, nil
}

type EntriesResponse struct {
	Items []map[string]any `json:"items"`
}
