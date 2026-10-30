package lms

import (
	"encoding/json"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/require"
)

func TestLmsProfileDefaults(t *testing.T) {
	for _, vendor := range []string{"Alma", "Sierra", "Koha", "FOLIO", "Generic"} {
		var cfg dirapi.LmsConfig
		require.NoError(t, json.Unmarshal([]byte(`{"vendor":"`+vendor+`","address":"https://ncip","fromAgency":"agency"}`), &cfg))
		adapter, err := CreateLmsAdapterNcip(cfg)
		require.NoError(t, err)
		a := adapter.(*LmsAdapterNcip)
		require.Equal(t, vendor != "Koha", *a.config.NcipNamespaceEnabled)
		if vendor == "Sierra" {
			require.Equal(t, "Hold", a.requestItemRequestType())
			require.Equal(t, "Title", a.requestItemRequestScopeType())
		} else {
			require.Equal(t, "Page", a.requestItemRequestType())
			if vendor == "Generic" {
				require.Equal(t, "auto", a.requestItemRequestScopeType())
			} else {
				require.Equal(t, "Item", a.requestItemRequestScopeType())
			}
		}
	}
}
func TestSierraNormalizationAndExplicitOverrides(t *testing.T) {
	for _, tc := range []struct{ id, normalization, want string }{{".b1234567", "sierra", "123456"}, {"1234567", "sierra", "123456"}, {".b123456x", "sierra", "123456x"}, {".b1234567", "none", ".b1234567"}} {
		var cfg dirapi.LmsConfig
		require.NoError(t, json.Unmarshal([]byte(`{"vendor":"Sierra","address":"https://ncip","fromAgency":"agency","bibIdNormalization":"`+tc.normalization+`","requestItemPickupLocationEnabled":false}`), &cfg))
		adapter, err := CreateLmsAdapterNcip(cfg)
		require.NoError(t, err)
		a := adapter.(*LmsAdapterNcip)
		client := new(ncipClientMock)
		a.ncipClient = client

		_, err = a.RequestItem("request", RequestItemIdentifier{BibliographicID: tc.id}, "user", "pickup", "")
		require.NoError(t, err)
		arg := client.lastRequest.(ncip.RequestItem)
		require.Equal(t, tc.want, arg.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
		require.Nil(t, arg.PickupLocation)
		require.Equal(t, "Hold", arg.RequestType.Text)
		require.Equal(t, "Title", arg.RequestScopeType.Text)
	}
}

func TestAdditionalLmsProfiles(t *testing.T) {
	for _, vendor := range []string{"Evergreen", "Horizon", "Polaris", "Symphony"} {
		for _, version := range []string{"", "1.0", "2.02"} {
			t.Run(vendor+version, func(t *testing.T) {
				var cfg dirapi.LmsConfig
				require.NoError(t, json.Unmarshal([]byte(`{"vendor":"`+vendor+`","address":"https://ncip","fromAgency":"agency"}`), &cfg))
				if version != "" {
					cfg.NcipVersion = &version
				}
				adapter, err := CreateLmsAdapterNcip(cfg)
				require.NoError(t, err)
				wantVersion := version
				if wantVersion == "" {
					wantVersion = "1.0"
					if vendor == "Evergreen" {
						wantVersion = "2.02"
					}
				}
				if wantVersion == "1.0" {
					a, ok := adapter.(*LmsAdapterNcip1)
					require.True(t, ok)
					require.Equal(t, "Loan", *a.config.RequestItemRequestType)
					require.Equal(t, "Bibliographic Item", *a.config.RequestItemRequestScopeType)
				} else {
					a, ok := adapter.(*LmsAdapterNcip)
					require.True(t, ok)
					require.Equal(t, "Loan", a.requestItemRequestType())
					require.Equal(t, "Bibliographic Item", a.requestItemRequestScopeType())
				}
				require.False(t, adapter.RequestItemUsesPickupLocation())
			})
		}
	}
}

func TestCirculationIdentifierNormalization(t *testing.T) {
	mode := "stripLeadingNonDigits"
	for _, tc := range []struct{ raw, want string }{
		{".b13088889", "13088889"}, {"cowcow4949", "4949"}, {"cow4949cow", "4949cow"}, {"12345", "12345"}, {"boogaboogabooga", "boogaboogabooga"}, {"", ""},
	} {
		require.Equal(t, tc.want, normalizeBibIdentifier(tc.raw, &mode))
	}
}
