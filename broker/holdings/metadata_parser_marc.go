package holdings

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/marcxml"
)

type MetadataParserMarc struct {
	config directory.MarcMetadataParserConfig
}

func NewMetadataParserMarc(config directory.MarcMetadataParserConfig) MetadataParser {
	if config.Identifier == nil && config.Title == nil && config.Isbn == nil && config.Issn == nil && config.Subtitle == nil && config.Author == nil && config.Edition == nil {
		config.Identifier = NewString("001")
		config.Title = NewString("245$a$n$p")
		config.Subtitle = NewString("245$b")
		config.Isbn = NewString("020$a")
		config.Issn = NewString("022$a")
		config.Author = NewString("100$a/100$?/110$a/110$?/111$a/111$?/245$c")
		config.Edition = NewString("250$a")
	}
	// perhaps should check if mainField is specified
	return &MetadataParserMarc{
		config: config,
	}
}

func (p *MetadataParserMarc) Parse(record []byte) (Metadata, error) {
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	if err != nil {
		var opacRecord marcxml.OpacRecord
		opacErr := xml.Unmarshal(record, &opacRecord)
		if opacErr != nil {
			return Metadata{}, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
		}
		content := opacRecord.BibliographicRecord.XMLContent
		err := xml.Unmarshal([]byte(content), &marcRecord)
		if err != nil {
			return Metadata{}, fmt.Errorf("failed to unmarshal MARC XML embedded in OPAC record: %w", err)
		}
	}

	var metadata Metadata
	entries := []struct {
		name        string
		configField *string
		store       *string
	}{
		{name: "Identifier", configField: p.config.Identifier, store: &metadata.Identifier},
		{name: "Title", configField: p.config.Title, store: &metadata.Title},
		{name: "Subtitle", configField: p.config.Subtitle, store: &metadata.Subtitle},
		{name: "Isbn", configField: p.config.Isbn, store: &metadata.Isbn},
		{name: "Issn", configField: p.config.Issn, store: &metadata.Issn},
		{name: "Author", configField: p.config.Author, store: &metadata.Author},
		{name: "Edition", configField: p.config.Edition, store: &metadata.Edition},
	}

	for _, e := range entries {
		if e.configField == nil {
			continue
		}
		if *e.configField == "" {
			return Metadata{}, fmt.Errorf("empty config field for %s", e.name)
		}
		altSplit := strings.Split(*e.configField, "/")
		for _, alt := range altSplit {
			*e.store = find(marcRecord, alt)
			if *e.store != "" {
				break
			}
		}
	}
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
		if ind1 != "" && field.Ind1 != ind1 {
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
