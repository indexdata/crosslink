package adapter

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type ApiDirectory struct {
	urls   []string
	client *http.Client
}

func CreateApiDirectory(client *http.Client, urls []string) DirectoryLookupAdapter {
	return &ApiDirectory{client: client, urls: urls}
}

func (a *ApiDirectory) GetDirectory(symbols []string, durl string) ([]DirectoryEntry, error, string) {
	cql := "symbol any"
	for _, s := range symbols {
		cql += " " + s
	}
	var dirEntries []DirectoryEntry
	query := "?maximumRecords=1000&cql=" + url.QueryEscape(cql)
	fullUrl := durl + query
	response, err := a.client.Get(fullUrl)
	if err != nil {
		return []DirectoryEntry{}, err, query
	}
	defer response.Body.Close()

	body := utils.Must(io.ReadAll(response.Body))
	if response.StatusCode != http.StatusOK {
		return []DirectoryEntry{}, fmt.Errorf("API returned non-OK status: %d, body: %s", response.StatusCode, body), query
	}

	var responseList EntriesResponse
	err = json.Unmarshal(body, &responseList)
	if err != nil {
		return []DirectoryEntry{}, err, query
	}
	childSymbolsById := make(map[string][]string, len(responseList.Items))
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
			if parent, ok := d["parent"].(string); ok {
				childSymbolsById[parent] = append(childSymbolsById[parent], symbols...)
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
			vendor := GetVendorFromUrl(apiUrl)
			entry := DirectoryEntry{
				Name:       d["name"].(string),
				Symbols:    symbols,
				Vendor:     vendor,
				BrokerMode: GetBrokerMode(vendor),
				URL:        apiUrl,
				CustomData: d,
			}
			dirEntries = append(dirEntries, entry)
		}
	}
	for i := range dirEntries {
		de := &dirEntries[i]
		if childSyms, ok := childSymbolsById[de.CustomData["id"].(string)]; ok {
			de.BranchSymbols = childSyms
		}
	}
	return dirEntries, nil, query
}

func (a *ApiDirectory) Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error, string) {
	var directoryList []DirectoryEntry
	var query string
	for _, durl := range a.urls {
		d, err, queryVal := a.GetDirectory(params.Symbols, durl)
		query = queryVal
		if err != nil {
			return []DirectoryEntry{}, err, query
		}
		directoryList = append(directoryList, d...)
	}
	return directoryList, nil, query
}

func (a *ApiDirectory) FilterAndSort(ctx extctx.ExtendedContext, entries []Supplier, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, RotaInfo) {
	var rotaInfo RotaInfo

	filtered := []Supplier{}
	requesterNetworks := getPeerNetworks(requesterData)
	var svcType string
	var svcLevel string
	maxCost := math.MaxFloat64 //TODO keeping original behavior, but should be set to 0.0 if no cost is specified
	if serviceInfo != nil {
		t := string(serviceInfo.ServiceType)
		svcType = strings.ToLower(t)
		if serviceInfo.ServiceLevel != nil {
			svcLevel = strings.ToLower(serviceInfo.ServiceLevel.Text)
		}
	}
	rotaInfo.Request.Type = svcType
	rotaInfo.Request.Level = svcLevel
	rotaInfo.Requester.Networks = make([]string, 0, len(requesterNetworks))
	for name := range requesterNetworks {
		rotaInfo.Requester.Networks = append(rotaInfo.Requester.Networks, name)
	}
	if billingInfo != nil && billingInfo.MaximumCosts != nil {
		maxCost = utils.Must(strconv.ParseFloat(utils.FormatDecimal(billingInfo.MaximumCosts.MonetaryValue.Base, billingInfo.MaximumCosts.MonetaryValue.Exp), 64))
		curSuffix := ""
		if billingInfo.MaximumCosts.CurrencyCode.Text != "" {
			curSuffix = " " + billingInfo.MaximumCosts.CurrencyCode.Text
		}
		rotaInfo.Request.Cost = utils.FormatDecimal(billingInfo.MaximumCosts.MonetaryValue.Base, billingInfo.MaximumCosts.MonetaryValue.Exp) + curSuffix
	}
	for _, sup := range entries {
		var supMatch SupplierMatch
		supMatch.Symbol = sup.Symbol
		supNetworks := getPeerNetworks(sup.CustomData)
		supMatch.Networks = make([]NetworkMatch, 0, len(supNetworks))
		for name := range supNetworks {
			supMatch.Networks = append(supMatch.Networks, NetworkMatch{
				Name:     name,
				Priority: supNetworks[name].Priority,
				Match:    false,
			})
		}
		priority := math.MaxInt
		for name := range requesterNetworks {
			if net, ok := supNetworks[name]; ok {
				if priority > net.Priority {
					priority = net.Priority
				}
				for i, n := range supMatch.Networks {
					if n.Name == name {
						supMatch.Networks[i].Match = true
					}
				}
			}
		}
		slices.SortFunc(supMatch.Networks, func(a, b NetworkMatch) int {
			if a.Match && !b.Match {
				return -1
			} else if !a.Match && b.Match {
				return 1
			}
			if a.Priority < b.Priority {
				return -1
			} else if a.Priority > b.Priority {
				return 1
			}
			return cmp.Compare(a.Name, b.Name)
		})
		if priority < math.MaxInt {
			sup.Priority = priority
			tiers := getPeerTiers(sup.CustomData)
			supMatch.Tiers = make([]TierMatch, 0, len(tiers))
			cost := math.MaxFloat64
			for _, t := range tiers {
				var tierMatch TierMatch
				tierMatch.Name = t.Name
				tierMatch.Level = strings.ToLower(t.Level)
				tierMatch.Type = strings.ToLower(t.Type)
				tierMatch.Cost = fmt.Sprintf("%.2f", t.Cost)

				sTypeMatch := svcType == "" || svcType == strings.ToLower(t.Type)
				sLevelMatch := svcLevel == "" || svcLevel == strings.ToLower(t.Level)
				costMatch := maxCost >= t.Cost

				if sTypeMatch && sLevelMatch && costMatch {
					tierMatch.Match = true
					if cost > t.Cost {
						cost = t.Cost
					}
				}
				supMatch.Tiers = append(supMatch.Tiers, tierMatch)
			}
			slices.SortFunc(supMatch.Tiers, func(a, b TierMatch) int {
				if a.Match && !b.Match {
					return -1
				} else if !a.Match && b.Match {
					return 1
				}
				if a.Cost < b.Cost {
					return -1
				} else if a.Cost > b.Cost {
					return 1
				}
				return cmp.Compare(a.Name, b.Name)
			})
			if cost < math.MaxFloat64 {
				supMatch.Match = true
				supMatch.Cost = fmt.Sprintf("%.2f", cost)
				sup.Cost = cost
				filtered = append(filtered, sup)
			}
			supMatch.Priority = sup.Priority
			supMatch.Local = sup.Local
			supMatch.Ratio = sup.Ratio
		}
		rotaInfo.Suppliers = append(rotaInfo.Suppliers, supMatch)
	}
	slices.SortFunc(rotaInfo.Suppliers, func(a, b SupplierMatch) int {
		if a.Match && !b.Match {
			return -1
		} else if !a.Match && b.Match {
			return 1
		}
		return CompareSuppliers(a, b)
	})
	slices.SortFunc(filtered, func(a, b Supplier) int {
		return CompareSuppliers(a, b)
	})
	return filtered, rotaInfo
}

func CompareSuppliers(a, b SupplierOrdering) int {
	if a.IsLocal() && !b.IsLocal() {
		return -1
	} else if !a.IsLocal() && b.IsLocal() {
		return 1
	}
	sort := cmp.Compare(a.GetCost(), b.GetCost())
	if sort != 0 {
		return sort
	}
	sort = cmp.Compare(a.GetPriority(), b.GetPriority())
	if sort != 0 {
		return sort
	}
	sort = cmp.Compare(a.GetRatio(), b.GetRatio())
	if sort != 0 {
		return sort
	}
	return cmp.Compare(a.GetSymbol(), b.GetSymbol())
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
						Priority: int(priority),
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

func GetVendorFromUrl(url string) extctx.Vendor {
	if strings.Contains(url, "alma.exlibrisgroup.com") {
		return extctx.VendorAlma
	} else if strings.Contains(url, "/rs/externalApi/iso18626") {
		return extctx.VendorReShare
	} else {
		return extctx.VendorUnknown
	}
}

func GetBrokerMode(vendor extctx.Vendor) extctx.BrokerMode {
	switch vendor {
	case extctx.VendorAlma:
		return extctx.BrokerModeOpaque
	case extctx.VendorReShare:
		return extctx.BrokerModeTransparent
	default:
		return DEFAULT_BROKER_MODE
	}
}

type EntriesResponse struct {
	Items []map[string]any `json:"items"`
}
