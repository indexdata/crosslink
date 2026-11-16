package catalog

import (
	"encoding/xml"
	"fmt"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/marcxml"
)

type MetadataParserMarc struct {
	config dirapi.MarcMetadataParserConfig
}

func NewMetadataParserMarc(config dirapi.MarcMetadataParserConfig) MetadataParser {
	if config.Identifier == nil {
		config.Identifier = NewString("001")
	}
	if config.Title == nil {
		config.Title = NewString("245$a$n$p")
	}
	if config.Subtitle == nil {
		config.Subtitle = NewString("245$b")
	}
	if config.Isbn == nil {
		config.Isbn = NewString("020$a")
	}
	if config.Issn == nil {
		config.Issn = NewString("022$a")
	}
	if config.Author == nil {
		config.Author = NewString("100$a/100$?/110$a/110$?/111$a/111$?/245$c")
	}
	if config.Edition == nil {
		config.Edition = NewString("250$a")
	}
	if config.Publisher == nil {
		config.Publisher = NewString("264*1$b/260$b")
	}
	if config.PublicationDate == nil {
		config.PublicationDate = NewString("264*1$c/260$c")
	}
	if config.PlaceOfPublication == nil {
		config.PlaceOfPublication = NewString("264*1$a/260$a")
	}
	return &MetadataParserMarc{
		config: config,
	}
}

func (p *MetadataParserMarc) Parse(record []byte) (Metadata, error) {
	// First, check if the record is an OPAC record and extract the MARC record from it
	var opacRecord marcxml.OpacRecord
	err := xml.Unmarshal(record, &opacRecord)
	if err == nil {
		record = opacRecord.BibliographicRecord.XMLContent
	}
	// Now parse the MARC record, try with the MARC21 slim namespace first, then without it if that fails
	var marcRecord marcxml.Record
	err = xml.Unmarshal(record, &marcRecord)
	if err != nil {
		// GVI marc does not have the MARC21 slim namespace, so we try again without it
		var noNamespaceRecord marcxml.RecordType
		err = xml.Unmarshal(record, &noNamespaceRecord)
		if err != nil {
			return Metadata{}, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
		}
		marcRecord.RecordType = noNamespaceRecord
	}
	var metadata Metadata
	entries := []struct {
		name        string
		configField *string
		store       *string
	}{
		{name: "Publisher", configField: p.config.Publisher, store: &metadata.Publisher},
		{name: "PublicationDate", configField: p.config.PublicationDate, store: &metadata.PublicationDate},
		{name: "PlaceOfPublication", configField: p.config.PlaceOfPublication, store: &metadata.PlaceOfPublication},
		{name: "Identifier", configField: p.config.Identifier, store: &metadata.Identifier},
		{name: "Title", configField: p.config.Title, store: &metadata.Title},
		{name: "Subtitle", configField: p.config.Subtitle, store: &metadata.Subtitle},
		{name: "Isbn", configField: p.config.Isbn, store: &metadata.Isbn},
		{name: "Issn", configField: p.config.Issn, store: &metadata.Issn},
		{name: "Author", configField: p.config.Author, store: &metadata.Author},
		{name: "Edition", configField: p.config.Edition, store: &metadata.Edition},
	}

	for _, e := range entries {
		if e.configField == nil || *e.configField == "" {
			continue
		}
		altSplit := strings.Split(*e.configField, "/")
		for _, alt := range altSplit {
			*e.store = find(marcRecord, alt)
			if *e.store != "" {
				break
			}
		}
	}
	metadata.PublicationType = marcPublicationType(marcRecord)
	return metadata, nil
}

func find(marcRecord marcxml.Record, spec string) string {
	splitSpec := strings.Split(spec, "$")
	var matchField string
	var ind1 string
	var ind2 string
	if len(splitSpec[0]) == 3 {
		matchField = splitSpec[0]
	} else if len(splitSpec[0]) == 5 {
		matchField = splitSpec[0][:3]
		ind1 = splitSpec[0][3:4]
		ind2 = splitSpec[0][4:5]
	}
	if len(splitSpec) == 1 {
		for _, field := range marcRecord.Controlfield {
			if field.Tag != matchField {
				continue
			}
			return strings.TrimSpace(string(field.Text))
		}
	}
	for _, field := range marcRecord.Datafield {
		if field.Tag != matchField {
			continue
		}
		if ind1 != "" && ind1 != "*" && field.Ind1 != ind1 {
			continue
		}
		if ind2 != "" && field.Ind2 != ind2 {
			continue
		}
		var values []string
		if len(splitSpec) < 2 {
			for _, subfield := range field.Subfield {
				values = append(values, strings.TrimSpace(string(subfield.Text)))
			}
		} else {
			for i := 1; i < len(splitSpec); i++ {
				for _, subfield := range field.Subfield {
					if splitSpec[i] == subfield.Code || splitSpec[i] == "?" {
						values = append(values, strings.TrimSpace(string(subfield.Text)))
					}
				}
			}
		}
		if len(values) > 0 {
			return strings.Join(values, " ")
		}
	}
	return ""
}

// marcPublicationType uses only unambiguous leader categories; unknown types stay absent.
func marcPublicationType(record marcxml.Record) string {
	if record.Leader == nil {
		return ""
	}
	leader := string(record.Leader.Text)
	if len(leader) < 8 {
		return ""
	}
	switch leader[6] {
	case 'a':
		switch leader[7] {
		case 'm':
			return "Book"
		case 'a':
			return "Chapter"
		case 'b':
			return "Article"
		}
	case 't':
		return "Manuscript"
	case 'c', 'd':
		return "MusicScore"
	case 'e', 'f':
		return "Map"
	case 'i':
		return "SoundRecording"
	case 'j':
		return "MusicRecording"
	}
	return ""
}
