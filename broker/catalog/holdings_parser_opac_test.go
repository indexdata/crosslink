package catalog

import (
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpacHoldingsParserReadsItemLoanPolicy(t *testing.T) {
	record := []byte(`<opacRecord>
  <bibliographicRecord/>
  <holdings>
    <holding>
      <localLocation>MAIN</localLocation>
      <shelvingLocation>STACKS</shelvingLocation>
      <callNumber>QA 1</callNumber>
      <circulations>
        <circulation>
          <availableNow value="0"/>
          <availableThru>IGNORED</availableThru>
          <itemId>unavailable-item</itemId>
          <renewable value="0"/>
          <onHold value="0"/>
        </circulation>
        <circulation>
          <availableNow value="1"/>
          <availableThru>  NORMAL  </availableThru>
          <itemId>available-item</itemId>
          <renewable value="1"/>
          <onHold value="0"/>
        </circulation>
      </circulations>
    </holding>
  </holdings>
</opacRecord>`)
	parser := NewOpacHoldingsParser(dirapi.OpacHoldingsParserConfig{})

	holdings, err := parser.Parse(record, LookupParams{})

	require.NoError(t, err)
	if assert.Len(t, holdings, 1) {
		assert.Equal(t, "available-item", holdings[0].ItemId)
		assert.Equal(t, "NORMAL", holdings[0].ItemLoanPolicy)
	}
}

func TestOpacDeprecatedAllCirculationsIgnored(t *testing.T) {
	record := []byte(`<opacRecord><holdings><holding><localLocation>MAIN</localLocation><circulations>
 <circulation><availableNow value="1"/><itemId>first</itemId></circulation>
 <circulation><availableNow value="0"/><itemId>unavailable</itemId></circulation>
 <circulation><availableNow value="1"/><itemId>second</itemId></circulation>
 </circulations></holding>
 <holding><localLocation>EXCLUDED</localLocation><circulations>
 <circulation><availableNow value="1"/><itemId>excluded</itemId></circulation>
 </circulations></holding></holdings></opacRecord>`)
	for _, tc := range []struct {
		name       string
		configured *bool
	}{
		{"default", nil}, {"legacy false", new(false)}, {"legacy true", new(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := NewOpacHoldingsParser(dirapi.OpacHoldingsParserConfig{AllCirculations: tc.configured, ExcludedLocations: &[]string{"EXCLUDED"}})
			holdings, err := parser.Parse(record, LookupParams{})
			require.NoError(t, err)
			ids := make([]string, 0, len(holdings))
			for _, h := range holdings {
				ids = append(ids, h.ItemId)
			}
			require.Equal(t, []string{"first", "second"}, ids)
		})
	}
}
