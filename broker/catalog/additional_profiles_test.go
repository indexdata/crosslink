package catalog

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
)

func TestAdditionalProfileFixtures(t *testing.T) {
	// Source: mod-rs service/src/test/resources/zresponsexml. Expected values
	// follow its host LMS tests, retaining CrossLink's whitespace normalization.
	for _, tc := range []struct {
		profile, file string
		want          []Holding
	}{
		{"Evergreen", "evergreen-lake-agassiz-with-available-item-status.xml", []Holding{{Location: "HAWLEY", ShelvingLocation: "Main", CallNumber: "PAR", ItemId: "33500012563250"}}},
		{"Evergreen", "evergreen-north-west-with-available-item-status.xml", []Holding{{Location: "ROSEAU", ShelvingLocation: "Main", CallNumber: "E pb", ItemId: "35500003682026"}}},
		{"Evergreen", "evergreen-traverse-de-sioux-with-available-item-status.xml", []Holding{{Location: "AM", ShelvingLocation: "Children's Literature Area", CallNumber: "PZ 7 .S3912 Ho", ItemId: "30619003213957"}}},
		{"Evergreen", "evergreen-east-central-with-checked-out-item-status.xml", nil},
		{"Evergreen", "evergreen-east-central-with-in-transit-item-status.xml", nil},
		{"Polaris", "polaris-with-in-item-status.xml", []Holding{{Location: "Juvenile nonfiction shelves", ShelvingLocation: "Main Library", CallNumber: "j574.92 Sill", ItemId: "31256015853493"}}},
		{"Polaris", "polaris-with-out-item-status.xml", nil},
		{"Polaris", "polaris-with-held-item-status.xml", nil},
		{"Horizon", "horizon-jhu.xml", []Holding{{Location: "Eisenhower", CallNumber: "SH 349 .M34 1992", ItemLoanPolicy: "MSEL Books"}}},
		{"Horizon", "horizon-hennepin.xml", []Holding{{Location: "Minneapolis Central (1st floor General)", ShelvingLocation: "Adult Fiction Book (Stacks)", CallNumber: "YEVTUSH", ItemLoanPolicy: "Checked In"}}},
		{"Symphony", "symphony-stanford.xml", []Holding{{Location: "SAL3", ShelvingLocation: "STACKS", CallNumber: "HV6534 .V55 J36 2017", ItemLoanPolicy: "STKS-MONO"}}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile("testdata/host-profiles/" + tc.file)
			require.NoError(t, err)
			var response struct {
				Records []struct {
					Data struct {
						XML string `xml:",innerxml"`
					} `xml:"recordData"`
				} `xml:"records>record"`
			}
			require.NoError(t, xml.Unmarshal(data, &response))
			require.NotEmpty(t, response.Records)
			var got []Holding
			for _, record := range response.Records {
				h, err := profileParser(t, tc.profile).Parse([]byte(record.Data.XML), LookupParams{})
				require.NoError(t, err)
				got = append(got, h...)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSymphonyAvailabilityPredicates(t *testing.T) {
	for _, tc := range []struct {
		sub       string
		available bool
	}{
		{`<subfield code="b">STACKS</subfield>`, true},
		{`<subfield code="b"/>`, true}, // present means an occurrence, matching mod-rs.
		{``, false},
		{`<subfield code="b">RESERVES</subfield>`, false},
		{`<subfield code="b">CHECKEDOUT</subfield>`, false},
		{`<subfield code="b">MISSING</subfield>`, false},
		{`<subfield code="b">DISCARD</subfield>`, false},
		{`<subfield code="b">STACKS</subfield><subfield code="b">MISSING</subfield>`, false},
	} {
		h, err := profileParser(t, "Symphony").Parse([]byte(`<record><datafield tag="926"><subfield code="a">MAIN</subfield>`+tc.sub+`<subfield code="d"> BOOK </subfield></datafield></record>`), LookupParams{})
		require.NoError(t, err)
		if tc.available {
			require.Len(t, h, 1)
			require.Equal(t, "BOOK", h[0].ItemLoanPolicy)
		} else {
			require.Empty(t, h)
		}
	}
}

func TestHorizonPolicySourcesAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, location, policies, overrides string
		want                                []string
	}{
		{"both", "MAIN", `<shelvingData> BOOK </shelvingData><circulations><circulation><availableNow value="1"/><temporaryLocation> TEMP </temporaryLocation></circulation></circulations>`, "", []string{"BOOK", "TEMP"}},
		{"missing", "MAIN", `<circulations><circulation><availableNow value="1"/></circulation></circulations>`, "", nil},
		{"internet", "Internet", `<shelvingData>BOOK</shelvingData><circulations><circulation><availableNow value="1"/></circulation></circulations>`, "", nil},
		{"empty location", " ", `<shelvingData>BOOK</shelvingData><circulations><circulation><availableNow value="1"/></circulation></circulations>`, "", nil},
		{"clear exclusion", "Internet", `<shelvingData>BOOK</shelvingData><circulations><circulation><availableNow value="1"/></circulation></circulations>`, `"excludedLocations":[]`, []string{"BOOK"}},
		{"disable extraction", "MAIN", `<shelvingData>BOOK</shelvingData><circulations><circulation><availableNow value="1"/></circulation></circulations>`, `"includeItemLoanPolicy":false`, nil},
		{"optional policy", "MAIN", `<circulations><circulation><availableNow value="1"/></circulation></circulations>`, `"requireItemLoanPolicy":false`, []string{""}},
		{"clear sources", "MAIN", `<shelvingData>BOOK</shelvingData><circulations><circulation><availableNow value="1"/></circulation></circulations>`, `"itemLoanPolicySources":[],"requireItemLoanPolicy":false`, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(`{"catalogConfig":{"profile":"Horizon","holdingsFormat":{"opac":{`+tc.overrides+`}}}}`), &entry))
			e, err := profiles.Resolve(entry)
			require.NoError(t, err)
			p, err := getHoldingsParser(e.Catalog.HoldingsFormat)
			require.NoError(t, err)
			h, err := p.Parse([]byte(`<opacRecord><holdings><holding><localLocation>`+tc.location+`</localLocation><shelvingLocation>STACKS</shelvingLocation>`+tc.policies+`</holding></holdings></opacRecord>`), LookupParams{})
			require.NoError(t, err)
			var policies []string
			for _, v := range h {
				policies = append(policies, v.ItemLoanPolicy)
				require.Equal(t, "STACKS", v.ShelvingLocation)
			}
			require.Equal(t, tc.want, policies)
		})
	}
}

func TestAdditionalProfileQueries(t *testing.T) {
	for _, tc := range []struct{ profile, id, overrides, want string }{
		{"Evergreen", "123", "", `@attr 1=1016 "123"`},
		{"Symphony", "a12345", "", `@attr 1=1016 "^C12345"`},
		{"Symphony", "12345", "", `@attr 1=1016 "^C12345"`},
		{"Symphony", "abcdefgHIJKL", "", `@attr 1=1016 "^CabcdefgHIJKL"`},
		{"Symphony", `a12"34`, "", `@attr 1=1016 "^C12\"34"`},
		{"Symphony", "a12345", `"identifierNormalization":"none","identifierPrefix":""`, `@attr 1=1016 "a12345"`},
		{"Symphony", "a12345", `"type":"cql"`, `rec.id = "\^C12345"`},
		{"Symphony", "a12345", `"type":"cql","identifier":"local.id = {term}"`, `local.id = "\^C12345"`},
	} {
		t.Run(tc.profile+tc.id+tc.overrides, func(t *testing.T) {
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(`{"catalogConfig":{"profile":"`+tc.profile+`","queryConfig":{`+tc.overrides+`}}}`), &entry))
			e, err := profiles.Resolve(entry)
			require.NoError(t, err)
			q, err := NewQueryBuilderGen(e.Catalog.QueryConfig)
			require.NoError(t, err)
			cql, pqf, err := q.Build(LookupParams{Identifier: tc.id, Isbn: "978123"})
			require.NoError(t, err)
			queries := append(cql, pqf...)
			require.Equal(t, tc.want, queries[0])
			require.NotContains(t, queries[1], "^C")
			cql, pqf, err = q.Build(LookupParams{Isbn: "978123"})
			require.NoError(t, err)
			require.Len(t, append(cql, pqf...), 1)
		})
	}
}

func TestHorizonAllCirculations(t *testing.T) {
	record := `<opacRecord><holdings><holding><localLocation>MAIN</localLocation><shelvingData>BOOK</shelvingData><circulations>` + strings.Repeat(`<circulation><availableNow value="1"/></circulation>`, 2) + `</circulations></holding></holdings></opacRecord>`
	h, err := profileParser(t, "Horizon").Parse([]byte(record), LookupParams{})
	require.NoError(t, err)
	require.Len(t, h, 2)
}
