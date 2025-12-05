package prservice

import (
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/ncip"
)

// TODO : create IlsAdapter and make NcipAdapter an implmentation of that interface
type NcipAdapter struct {
	ncipClient ncipclient.NcipClient
	dirAdapter adapter.DirectoryLookupAdapter
}

func CreateNcipAdapter(ncipClient ncipclient.NcipClient, dirAdapter adapter.DirectoryLookupAdapter) NcipAdapter {
	return NcipAdapter{
		ncipClient: ncipClient,
		dirAdapter: dirAdapter,
	}
}

func (n *NcipAdapter) getNcipInfoFromSymbols(symbols []string) (map[string]any, error) {
	dirEntries, _, err := n.dirAdapter.Lookup(adapter.DirectoryLookupParams{
		Symbols: symbols,
	})
	if err != nil {
		return nil, err
	}
	// find first NCIP entry
	for _, entry := range dirEntries {
		customData := entry.CustomData
		if ncip, ok := customData["ncip"].(map[string]any); ok {
			return ncip, nil
		}
	}
	return nil, nil
}

func (n *NcipAdapter) LookupUser(symbols []string, user string, password string) (bool, error) {
	ncipInfo, err := n.getNcipInfoFromSymbols(symbols)
	if err != nil {
		return false, err
	}
	if ncipInfo == nil {
		// no NCIP info, nothing to do
		return true, nil
	}
	var authenticationInput []ncip.AuthenticationInput
	if password != "" {
		authenticationInput = append(authenticationInput, ncip.AuthenticationInput{
			AuthenticationInputData: password,
		})
	}
	arg := ncip.LookupUser{
		UserId:              &ncip.UserId{UserIdentifierValue: user},
		AuthenticationInput: authenticationInput,
	}
	return n.ncipClient.LookupUser(ncipInfo, arg)
}
