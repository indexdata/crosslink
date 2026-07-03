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
	}

	for _, field := range marcRecord.Controlfield {
		for _, e := range entries {
			if e.configField == nil {
				continue
			}
			altSplit := strings.Split(*e.configField, "/")
			for _, alt := range altSplit {
				splitField := strings.Split(alt, "$")
				if len(splitField) == 1 {
					if field.Tag == splitField[0] {
						*e.store = strings.TrimSpace(string(field.Text))
					}
				}
				if *e.store != "" {
					break
				}
			}
		}
	}

	for _, field := range marcRecord.Datafield {
		for _, e := range entries {
			if e.configField == nil || *e.store != "" {
				continue
			}
			altSplit := strings.Split(*e.configField, "/")
			for _, alt := range altSplit {
				splitField := strings.Split(alt, "$")
				if len(splitField) < 2 {
					continue
				}
				if field.Tag != splitField[0] {
					continue
				}
				var values []string
				for _, subfield := range field.Subfield {
					for i := 1; i < len(splitField); i++ {
						if splitField[i] == subfield.Code || splitField[i] == "?" {
							values = append(values, strings.TrimSpace(string(subfield.Text)))
						}
					}
				}
				if len(values) > 0 {
					*e.store = strings.Join(values, " ")
					break
				}
			}
		}
	}
	return metadata, nil
}
