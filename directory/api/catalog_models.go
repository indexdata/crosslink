package api

import "github.com/indexdata/crosslink/directory/apimodels"

type EntryVendor = apimodels.EntryVendor

const (
	Alma      = apimodels.Alma
	CrossLink = apimodels.CrossLink
	ILLiad    = apimodels.ILLiad
	ReShare   = apimodels.ReShare
	Unknown   = apimodels.Unknown
)

type HoldingsConfig = apimodels.HoldingsConfig
type CatalogConfig = apimodels.CatalogConfig
type HoldingsParserConfig = apimodels.HoldingsParserConfig
type MarcHoldingsParserConfig = apimodels.MarcHoldingsParserConfig
type MarcMetadataParserConfig = apimodels.MarcMetadataParserConfig
type MetadataParserConfig = apimodels.MetadataParserConfig
type MetadataUpdateMode = apimodels.MetadataUpdateMode

const (
	Auto    = apimodels.Auto
	Merge   = apimodels.Merge
	None    = apimodels.None
	Replace = apimodels.Replace
)

type OpacHoldingsParserConfig = apimodels.OpacHoldingsParserConfig
type QueryConfig = apimodels.QueryConfig
type QueryConfigType = apimodels.QueryConfigType

const (
	Cql = apimodels.Cql
	Pqf = apimodels.Pqf
)

type SruConfig = apimodels.SruConfig
type ZoomConfig = apimodels.ZoomConfig
