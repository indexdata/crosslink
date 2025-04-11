package adapter

import (
	"cmp"
	"encoding/json"
	"fmt"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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
			for _, s := range listMap {
				if itemMap, castOk := s.(map[string]any); castOk {
					typeS, typeOk := itemMap["type"].(string)
					add, addOk := itemMap["address"].(string)
					if typeOk && addOk && typeS == "ISO18626" {
						apiUrl = add
					}
				}
			}
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

func (a *ApiDirectory) FilterAndSort(ctx extctx.ExtendedContext, entries []Supplier, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) []Supplier {
	filtered := []Supplier{}
	requesterNetworks := getPeerNetworks(requesterData)
	sType := ""
	sLevel := ""
	var maxCost *float64
	if serviceInfo != nil {
		t := string(serviceInfo.ServiceType)
		sType = t
		if serviceInfo.ServiceLevel != nil {
			sLevel = serviceInfo.ServiceLevel.Text
		}
	}
	if billingInfo != nil && billingInfo.MaximumCosts != nil {
		floatV := utils.Must(strconv.ParseFloat(utils.FormatDecimal(billingInfo.MaximumCosts.MonetaryValue.Base, billingInfo.MaximumCosts.MonetaryValue.Exp), 32))
		maxCost = &floatV
	}
	for _, e := range entries {
		eNetworks := getPeerNetworks(e.CustomData)
		var priority *float64
		for name := range requesterNetworks {
			if net, ok := eNetworks[name]; ok {
				if priority == nil || *priority > net.Priority {
					priority = &net.Priority
				}
			}
		}
		if priority != nil {
			e.NetworkPriority = *priority
			tiers := getPeerTiers(e.CustomData)
			var cost *float64
			for _, t := range tiers {
				if (sType == "" || sType == t.Type) && (sLevel == "" || sLevel == t.Level) && (maxCost == nil || *maxCost >= t.Cost) {
					if cost == nil || *cost > t.Cost {
						cost = &t.Cost
					}
				}
			}
			if cost != nil {
				e.Cost = *cost
				filtered = append(filtered, e)
			}
		}
	}
	slices.SortFunc(filtered, CompareSuppliers)
	return filtered
}

func CompareSuppliers(a, b Supplier) int {
	sort := cmp.Compare(a.Cost, b.Cost)
	if sort != 0 {
		return sort
	}
	sort = cmp.Compare(a.NetworkPriority, b.NetworkPriority)
	if sort != 0 {
		return sort
	}
	return cmp.Compare(a.Ratio, b.Ratio)
}

func getPeerNetworks(peerData map[string]any) map[string]Network {
	networks := map[string]Network{}
	if listMap, ok := peerData["networks"].([]any); ok && len(listMap) > 0 {
		for _, s := range listMap {
			if itemMap, castOk := s.(map[string]any); castOk {
				name, nameOk := itemMap["name"].(string)
				priority, priorityOk := itemMap["priority"].(float64)
				if nameOk && priorityOk {
					networks[name] = Network{
						Name:     name,
						Priority: priority,
					}
				}
			}
		}
	}
	return networks
}

func getPeerTiers(peerData map[string]any) []Tier {
	tiers := []Tier{}
	if listMap, ok := peerData["tiers"].([]any); ok && len(listMap) > 0 {
		for _, s := range listMap {
			if itemMap, castOk := s.(map[string]any); castOk {
				name, nameOk := itemMap["name"].(string)
				level, levelOk := itemMap["level"].(string)
				t, tOk := itemMap["type"].(string)
				cost, costOk := itemMap["cost"].(float64)
				if nameOk && levelOk && tOk && costOk {
					tiers = append(tiers, Tier{
						Name:  name,
						Level: level,
						Type:  t,
						Cost:  cost,
					})
				}
			}
		}
	}
	return tiers
}

type EntriesResponse struct {
	Items []map[string]any `json:"items"`
}

type Network struct {
	Name     string  `json:"name"`
	Priority float64 `json:"priority"`
}

type Tier struct {
	Name  string  `json:"name"`
	Cost  float64 `json:"cost"`
	Level string  `json:"level"`
	Type  string  `json:"type"`
}
