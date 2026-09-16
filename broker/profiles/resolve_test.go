package profiles

import (
	"encoding/json"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
)

func entry(t *testing.T, data string) dirapi.Entry {
	t.Helper()
	var e dirapi.Entry
	require.NoError(t, json.Unmarshal([]byte(data), &e))
	return e
}
func TestProfiles(t *testing.T) {
	for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		t.Run(name, func(t *testing.T) {
			raw := entry(t, `{"vendor":"Alma","illConfig":{"iso18626Vendor":"ReShare"},"lmsConfig":{"vendor":"`+name+`"},"catalogConfig":{"zoom":{"address":"catalog:210"}}}`)
			before, _ := json.Marshal(raw)
			e, err := Resolve(raw)
			require.NoError(t, err)
			after, _ := json.Marshal(raw)
			require.Equal(t, string(before), string(after))
			require.Equal(t, name, e.CatalogProfile)
			require.Empty(t, e.LMS.Address)
			require.Empty(t, e.LMS.FromAgency)
			switch name {
			case "Generic":
				require.Equal(t, "852", *e.Catalog.HoldingsFormat.Marc.MainField)
				require.Equal(t, "usmarc", (*e.Catalog.Zoom.Options)["preferredRecordSyntax"])
			case "Koha":
				require.False(t, *e.LMS.NcipNamespaceEnabled)
				require.Equal(t, "952", *e.Catalog.HoldingsFormat.Marc.MainField)
				require.Equal(t, "usmarc", (*e.Catalog.Zoom.Options)["preferredRecordSyntax"])
			case "Sierra":
				require.Equal(t, "Hold", *e.LMS.RequestItemRequestType)
				require.Equal(t, "Title", *e.LMS.RequestItemRequestScopeType)
				require.Equal(t, "sierra", *e.LMS.BibIdNormalization)
				require.Equal(t, "publicNote", *e.Catalog.HoldingsFormat.Opac.AvailabilityRule)
			default:
				require.True(t, *e.Catalog.HoldingsFormat.Opac.RequireLocalLocation)
				require.Equal(t, "opac", (*e.Catalog.Zoom.Options)["preferredRecordSyntax"])
			}
		})
	}
}
func TestIndependentOverrides(t *testing.T) {
	raw := entry(t, `{"lmsConfig":{"vendor":"Sierra","requestItemPickupLocationEnabled":false,"requestItemRequestType":"","bibIdNormalization":"none"},"catalogConfig":{"profile":"Koha","zoom":{"address":"site","options":{"preferredRecordSyntax":"custom"}},"holdingsFormat":{"marc":{"callNumberSubField":"x","availability":[]}}}}`)
	e, err := Resolve(raw)
	require.NoError(t, err)
	require.Equal(t, "Koha", e.CatalogProfile)
	require.False(t, *e.LMS.RequestItemPickupLocationEnabled)
	require.Empty(t, *e.LMS.RequestItemRequestType)
	require.Equal(t, "none", *e.LMS.BibIdNormalization)
	require.Equal(t, "952", *e.Catalog.HoldingsFormat.Marc.MainField)
	require.Equal(t, "x", *e.Catalog.HoldingsFormat.Marc.CallNumberSubField)
	require.Empty(t, *e.Catalog.HoldingsFormat.Marc.Availability)
	require.Equal(t, "custom", (*e.Catalog.Zoom.Options)["preferredRecordSyntax"])
	require.Equal(t, "Koha", e.Origins["catalogConfig.holdingsFormat.marc.mainField"])
	require.Equal(t, "directory", e.Origins["lmsConfig.requestItemPickupLocationEnabled"])
}
func TestParserReplacement(t *testing.T) {
	for _, parser := range []string{"marc", "reservoir", "marc21plus1"} {
		e, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Sierra"},"catalogConfig":{"holdingsFormat":{"`+parser+`":{}}}}`))
		require.NoError(t, err)
		require.Nil(t, e.Catalog.HoldingsFormat.Opac)
		for key := range e.Origins {
			require.NotContains(t, key, ".opac.")
		}
	}
	e, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Koha"},"catalogConfig":{"holdingsFormat":{"opac":{}}}}`))
	require.NoError(t, err)
	require.Nil(t, e.Catalog.HoldingsFormat.Marc)
	require.Equal(t, "availableNow", *e.Catalog.HoldingsFormat.Opac.AvailabilityRule)
}
func TestFallbackAndProfileOnly(t *testing.T) {
	for _, data := range []string{`{}`, `{"vendor":"Alma","illConfig":{"iso18626Vendor":"Alma"}}`} {
		e, err := Resolve(entry(t, data))
		require.NoError(t, err)
		require.Equal(t, "Generic", e.CatalogProfile)
		require.Equal(t, "Generic", e.LMSVendor)
	}
	e, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Sierra"},"catalogConfig":{"profile":"Generic"}}`))
	require.NoError(t, err)
	require.Equal(t, "Generic", e.CatalogProfile)
	require.Nil(t, e.Catalog.Sru)
	require.Nil(t, e.Catalog.Zoom)
	require.Equal(t, "852", *e.Catalog.HoldingsFormat.Marc.MainField)
	e, err = Resolve(entry(t, `{"catalogConfig":{"profile":"Alma"}}`))
	require.NoError(t, err)
	require.Nil(t, e.LMS)
	require.Nil(t, e.Catalog.Zoom)
}
func TestZoomParserSyntax(t *testing.T) {
	for _, profile := range []string{"Generic", "Koha", "Alma"} {
		for _, tc := range []struct {
			name, holdings, parser, syntax string
		}{
			{"marc", `"marc":{}`, "marc", "usmarc"},
			{"opac", `"opac":{}`, "opac", "opac"},
			{"reservoir", `"reservoir":{}`, "reservoir", "usmarc"},
			{"marc21plus1", `"marc21plus1":{}`, "marc21plus1", "xml"},
		} {
			t.Run(profile+"/"+tc.name, func(t *testing.T) {
				raw := entry(t, `{"catalogConfig":{"profile":"`+profile+`","zoom":{"address":"catalog:210"},"holdingsFormat":{`+tc.holdings+`}}}`)
				before, err := json.Marshal(raw)
				require.NoError(t, err)
				e, err := Resolve(raw)
				require.NoError(t, err)
				h := asObject(e.Catalog.HoldingsFormat)
				require.Len(t, h, 1)
				require.Contains(t, h, tc.parser)
				require.Equal(t, tc.syntax, (*e.Catalog.Zoom.Options)["preferredRecordSyntax"])
				if profile == "Koha" && tc.parser == "marc" {
					require.Equal(t, "952", *e.Catalog.HoldingsFormat.Marc.MainField)
				}
				after, err := json.Marshal(raw)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}
func TestRejectConflictingHoldingsParsers(t *testing.T) {
	parsers := []string{"marc", "opac", "reservoir", "marc21plus1"}
	for _, profile := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		for i, first := range parsers {
			for _, second := range parsers[i+1:] {
				t.Run(profile+"/"+first+"/"+second, func(t *testing.T) {
					raw := entry(t, `{"catalogConfig":{"profile":"`+profile+`","holdingsFormat":{"`+first+`":{},"`+second+`":{}}}}`)
					before, err := json.Marshal(raw)
					require.NoError(t, err)
					_, err = Resolve(raw)
					require.ErrorContains(t, err, "exactly one parser")
					after, err := json.Marshal(raw)
					require.NoError(t, err)
					require.JSONEq(t, string(before), string(after))
				})
			}
		}
	}
}

func TestSruSchemaDefaults(t *testing.T) {
	for _, profile := range []string{"Generic", "Koha", "Alma"} {
		for _, tc := range []struct{ parser, schema string }{
			{"marc", "marcxml"},
			{"opac", "opac"},
			{"reservoir", "marcxml"},
			{"marc21plus1", "marcxml"},
		} {
			t.Run(profile+"/"+tc.parser, func(t *testing.T) {
				for _, explicit := range []bool{false, true} {
					raw := entry(t, `{"catalogConfig":{"profile":"`+profile+`","sru":{"address":"https://catalog/sru"},"holdingsFormat":{"`+tc.parser+`":{}}}}`)
					want := tc.schema
					if explicit {
						want = "custom"
						raw.CatalogConfig.Sru.RecordSchema = &want
					}
					e, err := Resolve(raw)
					require.NoError(t, err)
					require.Equal(t, want, *e.Catalog.Sru.RecordSchema)
					require.Nil(t, e.Catalog.Zoom)
				}
			})
		}
	}
}

func TestValidation(t *testing.T) {
	for _, data := range []string{
		`{"lmsConfig":{"vendor":"WMS"}}`, `{"catalogConfig":{"profile":"Aleph"}}`, `{"lmsConfig":{"vendor":"bad"}}`,
		`{"lmsConfig":{"vendor":"Sierra","address":"x"}}`,
		`{"catalogConfig":{"profile":"Alma","zoom":{"address":""}}}`,
		`{"catalogConfig":{"profile":"Koha","holdingsFormat":{"marc":{"availability":[{"operator":"equals","subField":"7"}]}}}}`,
		`{"catalogConfig":{"profile":"Alma","holdingsFormat":{"opac":{"availabilityRule":"bad"}}}}`,
	} {
		_, err := Resolve(entry(t, data))
		require.Error(t, err)
		require.Contains(t, err.Error(), "profile")
	}
}

func TestRejectConflictingCatalogEndpoints(t *testing.T) {
	_, err := Resolve(entry(t, `{"catalogConfig":{"sru":{"address":"https://catalog/sru"},"zoom":{"address":"catalog:210"}}}`))
	require.ErrorContains(t, err, "simultaneous sru and zoom endpoints")
}
func TestDiagnosticsProtectCredentials(t *testing.T) {
	effective, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Sierra","address":"https://secret-address","fromAgency":"secret-agency","fromAgencyAuthentication":"secret-password"},"catalogConfig":{"zoom":{"address":"secret-catalog","options":{"password":"secret-password"}}}}`))
	require.NoError(t, err)
	diagnostics, err := json.Marshal(effective.Diagnostics())
	require.NoError(t, err)
	require.NotContains(t, string(diagnostics), "secret-")
	require.Contains(t, string(diagnostics), "Sierra")
}
