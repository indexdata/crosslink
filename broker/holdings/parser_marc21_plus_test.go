package holdings

import (
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestMarc21Plus1HoldingsParser_ParseError(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="notterminated>a</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	_, err := parser.Parse(marcXML, params)
	assert.Error(t, err)
}

func TestMarc21Plus1HoldingsParser_no_namespace(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record>
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">a</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:ISIL123", holding.Symbol)
}

func TestMarc21Plus1HoldingsParser_Parse_da(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">a</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:ISIL123", holding.Symbol)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)
}

func TestMarc21Plus1HoldingsParser_Parse_db(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">b</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:ISIL123", holding.Symbol)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)
}

func TestMarc21Plus1HoldingsParser_Parse_dc(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">c</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:ISIL123", holding.Symbol)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
}

func TestMarc21Plus1HoldingsParser_Parse_dd(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">d</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)
}

func TestMarc21Plus1HoldingsParser_Parse_de(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">ISIL123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">e</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:ISIL123", holding.Symbol)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)
}

func TestMarc21Plus1HoldingsParser_Parse_multiple(t *testing.T) {
	parser := NewMarc21Plus1HoldingsParser()

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID123</subfield>
			<subfield code="b">123</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">e</subfield>
			<subfield code="k">http://example.com/holding1</subfield>
			<subfield code="1">ProduktSigel123</subfield>
		</datafield>
		<datafield tag="924" ind1="0" ind2=" ">
			<subfield code="a">LocalID124</subfield>
			<subfield code="b">124</subfield>
			<subfield code="c">Region1</subfield>
			<subfield code="d">e</subfield>
			<subfield code="k">http://example.com/holding2</subfield>
			<subfield code="1">ProduktSigel124</subfield>
		</datafield>
	</record>`)

	params := LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopy),
	}
	holdings, err := parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 2)
	holding := holdings[0]
	assert.Equal(t, "LocalID123", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:123", holding.Symbol)
	holding = holdings[1]
	assert.Equal(t, "LocalID124", holding.LocalIdentifier)
	assert.Equal(t, "ISIL:124", holding.Symbol)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeCopyOrLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 2)

	params = LookupParams{
		ServiceType: "",
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 2)

	params = LookupParams{
		ServiceType: string(iso18626.TypeServiceTypeLoan),
	}
	holdings, err = parser.Parse(marcXML, params)
	assert.NoError(t, err)
	assert.Len(t, holdings, 0)
}
