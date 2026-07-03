package holdings

import (
	"testing"

	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestMetadataParserMarcDefault(t *testing.T) {
	parser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
	    <controlfield tag="001">123456789</controlfield>
	    <controlfield tag="002">435</controlfield>
	    <datafield tag="020" ind1=" " ind2=" ">
	        <subfield code="a">978-3-16-148410-0</subfield>
	        <subfield code="z">xxxxx</subfield>
		</datafield>
	    <datafield tag="022" ind1=" " ind2=" ">
	        <subfield code="a">8732</subfield>
	        <subfield code="z">xxxxx</subfield>
		</datafield>
		<datafield tag="245" ind1=" " ind2=" ">
	        <subfield code="a">The Title</subfield>
		</datafield>
	</record>`)

	metadata, err := parser.Parse(marcXML)
	assert.NoError(t, err)
	assert.Equal(t, "123456789", metadata.Identifier)
	assert.Equal(t, "978-3-16-148410-0", metadata.Isbn)
	assert.Equal(t, "8732", metadata.Issn)
	assert.Equal(t, "The Title", metadata.Title)
}

func TestMetadataParserMarcOverride(t *testing.T) {
	parser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{
		Identifier: NewString("002"),
		Title:      NewString("245$a"),
		Isbn:       NewString("020$a"),
		Issn:       NewString("022$a"),
	})

	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
	    <controlfield tag="001">123456789</controlfield>
	    <controlfield tag="002">435</controlfield>
	    <datafield tag="020" ind1=" " ind2=" ">
	        <subfield code="a">978-3-16-148410-0</subfield>
	        <subfield code="z">xxxxx</subfield>
		</datafield>
	    <datafield tag="022" ind1=" " ind2=" ">
	        <subfield code="a">8732</subfield>
	        <subfield code="z">xxxxx</subfield>
		</datafield>
		<datafield tag="245" ind1=" " ind2=" ">
	        <subfield code="a">The Title</subfield>
		</datafield>
	</record>`)

	metadata, err := parser.Parse(marcXML)
	assert.NoError(t, err)
	assert.Equal(t, "435", metadata.Identifier)
	assert.Equal(t, "978-3-16-148410-0", metadata.Isbn)
	assert.Equal(t, "8732", metadata.Issn)
	assert.Equal(t, "The Title", metadata.Title)
}

func TestMetadataParserMarcEmptyConfigField(t *testing.T) {
	empty := ""
	parser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{
		Identifier: &empty,
	})
	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
	    <controlfield tag="001">123456789</controlfield>
	</record>`)
	_, err := parser.Parse(marcXML)
	assert.ErrorContains(t, err, "empty config field for Identifier")
}

func TestMetadataParserMarcBadConfigField(t *testing.T) {
	content := "245$a$b"
	parser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{
		Identifier: &content,
	})
	marcXML := []byte(`
	<record xmlns="http://www.loc.gov/MARC21/slim">
	    <controlfield tag="001">123456789</controlfield>
	</record>`)
	_, err := parser.Parse(marcXML)
	assert.ErrorContains(t, err, "invalid config field for Identifier")
}

func TestMetadataParserBadXml(t *testing.T) {
	parser := NewMetadataParserMarc(directory.MarcMetadataParserConfig{})
	marcXML := []byte(`<xrecord`)
	_, err := parser.Parse(marcXML)
	assert.ErrorContains(t, err, "failed to unmarshal MARC XML")
}
