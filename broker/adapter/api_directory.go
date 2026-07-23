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

	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

const COMP = "api_directory"

type ApiDirectory struct {
	urls   []string
	client *http.Client
}

const defAuthority = "ISIL"

func CreateApiDirectory(client *http.Client, urls []string) DirectoryLookupAdapter {
	return &ApiDirectory{client: client, urls: urls}
}

func (a *ApiDirectory) getDirectory(ctx common.ExtendedContext, symbols []string, tenant string, durl string) ([]DirectoryEntry, string, error) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	var cql string
	if len(symbols) > 0 {
		cql = "symbol any \"" + cqlbuilder.EscapeMaskingChars(cqlbuilder.EscapeSpecialChars(strings.Join(symbols, " "))) + "\""
	}
	if tenant != "" {
		if cql != "" {
			cql += " and "
		}
		cql += "tenant=\"" + cqlbuilder.EscapeMaskingChars(cqlbuilder.EscapeSpecialChars(tenant)) + "\""
	}
	if cql == "" {
		return []DirectoryEntry{}, "", fmt.Errorf("no symbols or tenant provided for directory lookup")
	}
	var dirEntries []DirectoryEntry
	query := "?limit=1000&cql=" + url.QueryEscape(cql)
	fullUrl := durl + query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullUrl, nil)
	if err != nil {
		return []DirectoryEntry{}, query, err
	}
	req.Header.Set("X-Okapi-Permissions", `["directory.system.all"]`)
	if tenant != "" {
		req.Header.Set("X-Okapi-Tenant", tenant)
	}
	response, err := a.client.Do(req)
	if err != nil {
		return []DirectoryEntry{}, query, err
	}
	defer response.Body.Close()

	body := utils.Must(io.ReadAll(response.Body))
	if response.StatusCode != http.StatusOK {
		return []DirectoryEntry{}, query, fmt.Errorf("API returned non-OK status: %d, body: %s", response.StatusCode, body)
	}

	var responseList dirapi.EntriesResponse
	err = json.Unmarshal(body, &responseList)
	if err != nil {
		return []DirectoryEntry{}, query, err
	}
	childSymbolsById := make(map[string][]string, len(responseList.Items))
	for _, d := range responseList.Items {
		var symbols []string
		if d.Symbols != nil {
			for _, s := range *d.Symbols {
				authority := s.Authority
				if authority == "" {
					authority = defAuthority
				}
				symbols = append(symbols, authority+":"+s.Symbol)
			}
			if d.Parent != nil {
				parentID := d.Parent.String()
				childSymbolsById[parentID] = append(childSymbolsById[parentID], symbols...)
			}
		}
		if len(symbols) == 0 {
			ctx.Logger().Info("Directory entry has no symbols and will be ignored", "entryName", d.Name)
			continue
		}
		apiUrl := ""
		if d.Endpoints != nil {
			for _, s := range *d.Endpoints {
				if s.Type == "ISO18626" && s.Address != "" {
					apiUrl = s.Address
				}
			}
		}
		vendor := dirapi.Unknown
		if d.Vendor != nil {
			vendor = *d.Vendor
		} else if apiUrl != "" {
			vendor = GetVendorFromUrl(apiUrl)
		}
		entry := DirectoryEntry{
			Name:       d.Name,
			Symbols:    symbols,
			Vendor:     vendor,
			BrokerMode: GetBrokerMode(vendor),
			URL:        apiUrl,
			CustomData: d,
		}
		dirEntries = append(dirEntries, entry)
	}
	for i := range dirEntries {
		de := &dirEntries[i]
		if de.CustomData.Id != nil {
			id := (*de.CustomData.Id).String()
			if childSyms, ok := childSymbolsById[id]; ok {
				de.BranchSymbols = childSyms
			}
		}
	}
	return dirEntries, query, nil
}

func (a *ApiDirectory) Lookup(ctx common.ExtendedContext, params DirectoryLookupParams) ([]DirectoryEntry, string, error) {
	var directoryList []DirectoryEntry
	var query string
	for _, durl := range a.urls {
		d, queryVal, err := a.getDirectory(ctx, params.Symbols, params.Tenant, durl)
		query = queryVal
		if err != nil {
			return []DirectoryEntry{}, query, err
		}
		directoryList = append(directoryList, d...)
	}
	return directoryList, query, nil
}

func (a *ApiDirectory) FilterAndSort(ctx common.ExtendedContext, entries []Supplier, requesterData dirapi.Entry, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, RotaInfo) {
	var rotaInfo RotaInfo

	filtered := []Supplier{}
	reqNetworks := getPeerNetworks(requesterData)
	reqTiers := getPeerTiers(requesterData)
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
	rotaInfo.Requester.Networks = make([]string, 0, len(reqNetworks))
	for name := range reqNetworks {
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
		sharedPriority := math.MaxInt
		for name, reqNet := range reqNetworks {
			if _, ok := supNetworks[name]; ok {
				if sharedPriority > reqNet.Priority {
					sharedPriority = reqNet.Priority
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
		if sharedPriority < math.MaxInt {
			sup.Priority = sharedPriority
			suppTiers := getPeerTiers(sup.CustomData)
			supMatch.Tiers = make([]TierMatch, 0, len(suppTiers))
			cost := math.MaxFloat64
			priority := math.MaxInt
			for _, suppTier := range suppTiers {
				var tierMatch TierMatch
				tierMatch.Name = suppTier.Name
				tierMatch.Level = strings.ToLower(suppTier.Level)
				tierMatch.Type = strings.ToLower(suppTier.Type)
				tierMatch.Cost = fmt.Sprintf("%.2f", suppTier.Cost)

				suppTypeMatch := svcType == "" || svcType == strings.ToLower(suppTier.Type)
				suppLevelMatch := svcLevel == "" || svcLevel == strings.ToLower(suppTier.Level)
				suppCostMatch := costMatches(suppTier.Cost, maxCost)
				tierPriority := sharedNetworkPriorityForCost(suppTier.Cost, reqNetworks, supNetworks)
				suppNetworkMatch := tierPriority < math.MaxInt

				if suppTypeMatch && suppLevelMatch && suppCostMatch && suppNetworkMatch {
					reciprocal := true
					//supplier tier matched the request, if the tier is free it must be reciprocal
					if suppTier.Cost == 0 {
						reciprocal = false
						for _, reqTier := range reqTiers {
							reqTypeMatch := reqTier.Type == "" || strings.EqualFold(reqTier.Type, suppTier.Type)
							reqLevelMatch := reqTier.Level == "" || strings.EqualFold(reqTier.Level, suppTier.Level)
							reqCostMatch := suppTier.Cost == reqTier.Cost
							if reqTypeMatch && reqLevelMatch && reqCostMatch {
								reciprocal = true
								break
							}
						}
					}
					tierMatch.Match = reciprocal
					if reciprocal && (cost > suppTier.Cost || cost == suppTier.Cost && priority > tierPriority) {
						cost = suppTier.Cost
						priority = tierPriority
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
				sup.Priority = priority
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

func costMatches(suppCost, maxCost float64) bool {
	if maxCost > 0 && maxCost < math.MaxFloat64 {
		// cost is specified, we are in pay for peer mode
		return suppCost > 0 && suppCost <= maxCost
	} else {
		// no cost or zero, reciprocal mode
		return suppCost == 0
	}
}

func sharedNetworkPriorityForCost(suppCost float64, reqNetworks, supNetworks map[string]Network) int {
	priority := math.MaxInt
	for name, reqNet := range reqNetworks {
		supNet, ok := supNetworks[name]
		if !ok {
			continue
		}
		if networkAllowsCost(reqNet, suppCost) && networkAllowsCost(supNet, suppCost) {
			if priority > reqNet.Priority {
				priority = reqNet.Priority
			}
		}
	}
	return priority
}

func networkAllowsCost(network Network, cost float64) bool {
	if network.Reciprocal == nil {
		return true
	}
	if *network.Reciprocal {
		return cost == 0
	}
	return cost > 0
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

func getPeerNetworks(peerData dirapi.Entry) map[string]Network {
	networks := map[string]Network{}
	if peerData.Networks != nil {
		for _, n := range *peerData.Networks {
			if n.Name != nil {
				networks[*n.Name] = Network{
					Name:       *n.Name,
					Priority:   int(n.Priority),
					Reciprocal: nil,
				}
			}
		}
	}
	return networks
}

func getPeerTiers(peerData dirapi.Entry) []Tier {
	tiers := []Tier{}
	if peerData.Tiers != nil {
		for _, t := range *peerData.Tiers {
			name := ""
			if t.Name != nil {
				name = *t.Name
			}
			tiers = append(tiers, Tier{
				Name:  name,
				Level: string(t.Level),
				Type:  string(t.Type),
				Cost:  t.Cost,
			})
		}
	}
	return tiers
}

func GetVendorFromUrl(url string) dirapi.EntryVendor {
	url = strings.ToLower(url)
	if strings.Contains(url, "alma.exlibrisgroup.com") || strings.Contains(url, "rapido.exlibrisgroup.com") {
		return dirapi.Alma
	} else if strings.Contains(url, "/rs/externalapi/iso18626") {
		return dirapi.ReShare
	} else if strings.Contains(url, "atlas-sys.com") || strings.Contains(url, "illiad") {
		return dirapi.ILLiad
	} else {
		return dirapi.Unknown
	}
}

func GetBrokerMode(vendor dirapi.EntryVendor) common.BrokerMode {
	switch vendor {
	case dirapi.Alma:
		return common.BrokerModeOpaque
	case dirapi.ILLiad:
		return common.BrokerModeOpaque
	case dirapi.ReShare:
		return common.BrokerModeTransparent
	case dirapi.CrossLink:
		return common.BrokerModeTransparent
	default:
		return DEFAULT_BROKER_MODE
	}
}
