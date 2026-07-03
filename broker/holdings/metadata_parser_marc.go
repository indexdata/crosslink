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
	if config.Identifier == nil && config.Title == nil && config.Isbn == nil && config.Issn == nil {
		config.Identifier = NewString("001")
		config.Title = NewString("245$a")
		config.Isbn = NewString("020$a")
		config.Issn = NewString("022$a")
	}
	// perhaps should check if mainField is specified
	return &MetadataParserMarc{
		config: config,
	}
}

func (p *MetadataParserMarc) Parse(record []byte) (Metadata, error) {
	var marcRecord marcxml.Record
	err := xml.Unmarshal(record, &marcRecord)
	// TODO : consider OPAC record as well
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to unmarshal MARC XML: %w", err)
	}

	var metadata Metadata
	entries := []struct {
		name        string
		configField *string
		store       *string
	}{
		{name: "Identifier", configField: p.config.Identifier, store: &metadata.Identifier},
		{name: "Isbn", configField: p.config.Isbn, store: &metadata.Isbn},
		{name: "Issn", configField: p.config.Issn, store: &metadata.Issn},
		{name: "Title", configField: p.config.Title, store: &metadata.Title},
	}

	for _, e := range entries {
		if e.configField == nil {
			continue
		}
		if *e.configField == "" {
			return Metadata{}, fmt.Errorf("empty config field for %s", e.name)
		}
		splitField := strings.Split(*e.configField, "$")
		if len(splitField) > 2 {
			return Metadata{}, fmt.Errorf("invalid config field for %s: %s", e.name, *e.configField)
		}
	}

	for _, field := range marcRecord.Controlfield {
		for _, e := range entries {
			if e.configField == nil {
				continue
			}
			splitField := strings.Split(*e.configField, "$")
			if len(splitField) == 1 {
				if field.Tag == splitField[0] {
					*e.store = strings.TrimSpace(string(field.Text))
				}
			}
		}
	}

	for _, field := range marcRecord.Datafield {
		for _, e := range entries {
			if e.configField == nil {
				continue
			}
			splitField := strings.Split(*e.configField, "$")
			if len(splitField) != 2 {
				continue
			}
			if field.Tag != splitField[0] {
				continue
			}
			for _, subfield := range field.Subfield {
				if subfield.Code == splitField[1] {
					*e.store = strings.TrimSpace(string(subfield.Text))
				}
			}
		}
	}
	return metadata, nil
}
