package api

// EntryVendor identifies the ILL platform inferred for an entry.
type EntryVendor string

const (
	Alma      EntryVendor = "Alma"
	CrossLink EntryVendor = "CrossLink"
	ILLiad    EntryVendor = "ILLiad"
	ReShare   EntryVendor = "ReShare"
	Unknown   EntryVendor = "Unknown"
)

func (e EntryVendor) Valid() bool {
	switch e {
	case Alma, CrossLink, ILLiad, ReShare, Unknown:
		return true
	default:
		return false
	}
}

// CatalogConfig contains catalog lookup configuration used by broker.
type CatalogConfig struct {
	HoldingsFormat     *HoldingsParserConfig `json:"holdingsFormat,omitempty"`
	MetadataFormat     *MetadataParserConfig `json:"metadataFormat,omitempty"`
	MetadataUpdateMode *MetadataUpdateMode   `json:"metadataUpdateMode,omitempty"`
	QueryConfig        *QueryConfig          `json:"queryConfig,omitempty"`
	Sru                *SruConfig            `json:"sru,omitempty"`
	Zoom               *ZoomConfig           `json:"zoom,omitempty"`
}

type HoldingsParserConfig struct {
	Marc        *MarcHoldingsParserConfig `json:"marc,omitempty"`
	Marc21plus1 *map[string]interface{}   `json:"marc21plus1,omitempty"`
	Opac        *OpacHoldingsParserConfig `json:"opac,omitempty"`
	Reservoir   *map[string]interface{}   `json:"reservoir,omitempty"`
}

type MarcHoldingsParserConfig struct {
	CallNumberSubField       *string `json:"callNumberSubField,omitempty"`
	ItemIdSubField           *string `json:"itemIdSubField,omitempty"`
	LocationSubField         *string `json:"locationSubField,omitempty"`
	MainField                *string `json:"mainField,omitempty"`
	RestrictedSubField       *string `json:"restrictedSubField,omitempty"`
	ShelvingLocationSubField *string `json:"shelvingLocationSubField,omitempty"`
}

type MarcMetadataParserConfig struct {
	Author     *string `json:"author,omitempty"`
	Edition    *string `json:"edition,omitempty"`
	Identifier *string `json:"identifier,omitempty"`
	Isbn       *string `json:"isbn,omitempty"`
	Issn       *string `json:"issn,omitempty"`
	Subtitle   *string `json:"subtitle,omitempty"`
	Title      *string `json:"title,omitempty"`
}

type MetadataParserConfig struct {
	Marc21 *MarcMetadataParserConfig `json:"marc21,omitempty"`
}

type MetadataUpdateMode string

const (
	Auto    MetadataUpdateMode = "auto"
	Merge   MetadataUpdateMode = "merge"
	None    MetadataUpdateMode = "none"
	Replace MetadataUpdateMode = "replace"
)

func (e MetadataUpdateMode) Valid() bool {
	switch e {
	case Auto, Merge, None, Replace:
		return true
	default:
		return false
	}
}

type OpacHoldingsParserConfig = map[string]interface{}

type QueryConfig struct {
	Identifier *string          `json:"identifier,omitempty"`
	Isbn       *string          `json:"isbn,omitempty"`
	Issn       *string          `json:"issn,omitempty"`
	Title      *string          `json:"title,omitempty"`
	Type       *QueryConfigType `json:"type,omitempty"`
}

type QueryConfigType string

const (
	Cql QueryConfigType = "cql"
	Pqf QueryConfigType = "pqf"
)

func (e QueryConfigType) Valid() bool {
	switch e {
	case Cql, Pqf:
		return true
	default:
		return false
	}
}

type SruConfig struct {
	Address      string  `json:"address"`
	RecordSchema *string `json:"recordSchema,omitempty"`
}

type ZoomConfig struct {
	Address string             `json:"address"`
	Options *map[string]string `json:"options,omitempty"`
}
