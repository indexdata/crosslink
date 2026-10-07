package lms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/profiles"
	"github.com/indexdata/crosslink/broker/test/profiletest"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLmsProfileDefaults(t *testing.T) {
	for _, vendor := range []string{"Alma", "Sierra", "Koha", "FOLIO", "Generic"} {
		var cfg dirapi.LmsConfig
		require.NoError(t, json.Unmarshal([]byte(`{"vendor":"`+vendor+`","address":"https://ncip","fromAgency":"agency"}`), &cfg))
		adapter, err := CreateLmsAdapterNcip(cfg, profiletest.NewResolver(t))
		require.NoError(t, err)
		a := adapter.(*LmsAdapterNcip)
		require.Equal(t, vendor != "Koha", *a.config.NcipNamespaceEnabled)
		if vendor == "Sierra" {
			require.Equal(t, "Hold", a.requestItemRequestType())
			require.Equal(t, "Title", a.requestItemRequestScopeType())
		} else {
			require.Equal(t, "Page", a.requestItemRequestType())
		}
	}
}
func TestSierraNormalizationAndExplicitOverrides(t *testing.T) {
	for _, tc := range []struct{ id, normalization, want string }{{".b1234567", "sierra", "123456"}, {"1234567", "sierra", "123456"}, {".b123456x", "sierra", "123456x"}, {".b1234567", "none", ".b1234567"}} {
		var cfg dirapi.LmsConfig
		require.NoError(t, json.Unmarshal([]byte(`{"vendor":"Sierra","address":"https://ncip","fromAgency":"agency","bibIdNormalization":"`+tc.normalization+`","requestItemPickupLocationEnabled":false}`), &cfg))
		adapter, err := CreateLmsAdapterNcip(cfg, profiletest.NewResolver(t))
		require.NoError(t, err)
		a := adapter.(*LmsAdapterNcip)
		client := new(ncipClientMock)
		a.ncipClient = client

		_, err = a.RequestItem("request", tc.id, "user", "pickup", "")
		require.NoError(t, err)
		arg := client.lastRequest.(ncip.RequestItem)
		require.Equal(t, tc.want, arg.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
		require.Nil(t, arg.PickupLocation)
		require.Equal(t, "Hold", arg.RequestType.Text)
		require.Equal(t, "Title", arg.RequestScopeType.Text)
	}
}

func TestDirectorySnapshotChangesLmsDefaults(t *testing.T) {
	spec, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(spec, &document))
	document["x-host-profiles"].(map[string]any)["Sierra"].(map[string]any)["lmsConfig"].(map[string]any)["requestItemRequestType"] = "Page"
	spec, err = json.Marshal(document)
	require.NoError(t, err)
	resolver, err := profiles.NewResolver(spec)
	require.NoError(t, err)
	var cfg dirapi.LmsConfig
	require.NoError(t, json.Unmarshal([]byte(`{"vendor":"Sierra","address":"https://ncip","fromAgency":"agency"}`), &cfg))
	adapter, err := CreateLmsAdapterNcip(cfg, resolver)
	require.NoError(t, err)
	require.Equal(t, "Page", adapter.(*LmsAdapterNcip).requestItemRequestType())
	repo := &MockIllRepo{}
	repo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{{CustomData: dirapi.Entry{LmsConfig: &cfg}}}, "", nil)
	adapter, err = NewLmsCreator(repo, nil, resolver).GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), "ISIL:MAIN")
	require.NoError(t, err)
	require.Equal(t, "Page", adapter.(*LmsAdapterNcip).requestItemRequestType())
}
