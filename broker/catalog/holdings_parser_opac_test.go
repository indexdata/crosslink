package catalog

import (
	"testing"

	"github.com/indexdata/crosslink/directory"
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
	parser := NewOpacHoldingsParser(directory.OpacHoldingsParserConfig{})

	holdings, err := parser.Parse(record, LookupParams{})

	require.NoError(t, err)
	if assert.Len(t, holdings, 1) {
		assert.Equal(t, "available-item", holdings[0].ItemId)
		assert.Equal(t, "NORMAL", holdings[0].ItemLoanPolicy)
	}
}
