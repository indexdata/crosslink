package catalog

import (
	"github.com/indexdata/crosslink/marcxml"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
)

func TestMetadataParserMarcDefault(t *testing.T) {
	parser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{})

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
		<datafield tag="100" ind1=" " ind2=" ">
	        <subfield code="a">John Doe</subfield>
		</datafield>
		<datafield tag="245" ind1=" " ind2=" ">
	        <subfield code="a">The Title</subfield>
	        <subfield code="b">The Subtitle</subfield>
	        <subfield code="n">Section</subfield>
		</datafield>
		<datafield tag="250" ind1=" " ind2=" ">
	        <subfield code="a">1st edition</subfield>
		</datafield>
	</record>`)

	metadata, err := parser.Parse(marcXML)
	assert.NoError(t, err)
	assert.Equal(t, "123456789", metadata.Identifier)
	assert.Equal(t, "978-3-16-148410-0", metadata.Isbn)
	assert.Equal(t, "8732", metadata.Issn)
	assert.Equal(t, "The Title Section", metadata.Title)
	assert.Equal(t, "The Subtitle", metadata.Subtitle)
	assert.Equal(t, "John Doe", metadata.Author)
	assert.Equal(t, "1st edition", metadata.Edition)
}

func TestMetadataParserMarcOverride(t *testing.T) {
	empty := ""
	parser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{
		Identifier: NewString("002"),
		Title:      NewString("245$a$n$p"),
		Isbn:       NewString("020$a"),
		Issn:       &empty, // so ISSN will not be parsed at all
		Subtitle:   NewString("245$b"),
		Author:     NewString("100$a/100$?/245$c/110$a/110$?/111$a/111$?"),
		Edition:    NewString("250"),
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
		<datafield tag="110" ind1=" " ind2=" ">
	        <subfield code="b">John Doe</subfield>
		</datafield>
		<datafield tag="245" ind1=" " ind2=" ">
	        <subfield code="a">The Title</subfield>
	        <subfield code="b">The Subtitle</subfield>
			<subfield code="c">John W Doe</subfield>
		</datafield>
		<datafield tag="250" ind1=" " ind2=" ">
	        <subfield code="b">2nd edition</subfield>
		</datafield>
	</record>`)

	metadata, err := parser.Parse(marcXML)
	assert.NoError(t, err)
	assert.Equal(t, "435", metadata.Identifier)
	assert.Equal(t, "978-3-16-148410-0", metadata.Isbn)
	assert.Equal(t, "", metadata.Issn)
	assert.Equal(t, "The Title", metadata.Title)
	assert.Equal(t, "John W Doe", metadata.Author)
	assert.Equal(t, "The Subtitle", metadata.Subtitle)
	assert.Equal(t, "2nd edition", metadata.Edition)
}

func TestMetadataParserMarcNoNamespace(t *testing.T) {
	parser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{})

	marcXML := []byte(`
	<record>
	    <controlfield tag="001">123456789</controlfield>
	    <controlfield tag="002">435</controlfield>
	    <datafield tag="020" ind1=" " ind2=" ">
	        <subfield code="a">978-3-16-148410-0</subfield>
	        <subfield code="z">xxxxx</subfield>
		</datafield>
	</record>`)

	metadata, err := parser.Parse(marcXML)
	assert.NoError(t, err)
	assert.Equal(t, "123456789", metadata.Identifier)
	assert.Equal(t, "978-3-16-148410-0", metadata.Isbn)
	assert.Equal(t, "", metadata.Issn)
	assert.Equal(t, "", metadata.Title)
	assert.Equal(t, "", metadata.Subtitle)
	assert.Equal(t, "", metadata.Author)
	assert.Equal(t, "", metadata.Edition)
}

func TestMetadataParserBadXml(t *testing.T) {
	parser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{})
	marcXML := []byte(`<xrecord`)
	_, err := parser.Parse(marcXML)
	assert.ErrorContains(t, err, "failed to unmarshal MARC XML")
}

func TestMetadataPublication(t *testing.T) {
	parser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{})
	record := []byte(`<record xmlns="http://www.loc.gov/MARC21/slim"><leader>00000nam a2200000 a 4500</leader>
 <datafield tag="264" ind1=" " ind2="4"><subfield code="c">copyright date</subfield></datafield>
 <datafield tag="260" ind1=" " ind2=" "><subfield code="a">Old place</subfield><subfield code="b">Old publisher</subfield><subfield code="c">1999</subfield></datafield>
 <datafield tag="264" ind1="3" ind2="1"><subfield code="a">Place</subfield><subfield code="b">Publisher</subfield><subfield code="c">[2025]</subfield></datafield></record>`)
	metadata, err := parser.Parse(record)
	assert.NoError(t, err)
	assert.Equal(t, "Publisher", metadata.Publisher)
	assert.Equal(t, "Place", metadata.PlaceOfPublication)
	assert.Equal(t, "[2025]", metadata.PublicationDate)
	assert.Equal(t, "Book", metadata.PublicationType)
	empty := ""
	parser = NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{Publisher: NewString("260$b"), PublicationDate: &empty})
	metadata, err = parser.Parse(record)
	assert.NoError(t, err)
	assert.Equal(t, "Old publisher", metadata.Publisher)
	assert.Empty(t, metadata.PublicationDate)
	metadata, err = parser.Parse([]byte(`<record><datafield tag="260"><subfield code="b">Legacy publisher</subfield></datafield></record>`))
	assert.NoError(t, err)
	assert.Equal(t, "Legacy publisher", metadata.Publisher)
	assert.Empty(t, metadata.PublicationType)
}

func TestPublicationTypeLeaderMapping(t *testing.T) {
	for _, tc := range []struct{ leader, want string }{
		{"00000nam", "Book"}, {"00000naa", "Chapter"}, {"00000nab", "Article"}, {"00000nas", ""},
		{"00000ntm", "Manuscript"}, {"00000ncm", "MusicScore"}, {"00000ndm", "MusicScore"},
		{"00000nem", "Map"}, {"00000nfm", "Map"}, {"00000nim", "SoundRecording"}, {"00000njm", "MusicRecording"},
		{"00000nkm", ""}, {"short", ""},
	} {
		t.Run(tc.leader, func(t *testing.T) {
			record := marcxml.Record{RecordType: marcxml.RecordType{Leader: &marcxml.LeaderFieldType{Text: marcxml.LeaderDataType(tc.leader)}}}
			assert.Equal(t, tc.want, marcPublicationType(record))
		})
	}
}
