package model

import "fmt"

type LMSConfig struct {
	Address                          string           `json:"address"`
	FromAgency                       string           `json:"fromAgency"`
	FromAgencyAuthentication         *string          `json:"fromAgencyAuthentication"`
	ToAgency                         *string          `json:"toAgency"`
	LookupUserEnabled                *bool            `json:"lookupUserEnabled"`
	AcceptItemEnabled                *bool            `json:"acceptItemEnabled"`
	CheckInItemEnabled               *bool            `json:"checkInItemEnabled"`
	CheckOutItemEnabled              *bool            `json:"checkOutItemEnabled"`
	ItemLocation                     *string          `json:"itemLocation"`
	RequestItemRequestType           *string          `json:"requestItemRequestType"`
	RequestItemRequestScopeType      *string          `json:"requestItemRequestScopeType"`
	RequestItemBibIDCode             *string          `json:"requestItemBibIdCode"`
	RequestItemEnabled               *bool            `json:"requestItemEnabled"`
	RequestItemPickupLocationEnabled *bool            `json:"requestItemPickupLocationEnabled"`
	RequesterPickupLocation          *string          `json:"requesterPickupLocation"`
	SupplierPickupLocation           *string          `json:"supplierPickupLocation"`
	RequesterPatronPattern           *string          `json:"requesterPatronPattern"`
	PatronProfiles                   *[]PatronProfile `json:"patronProfiles"`
}

type PatronProfile struct {
	Code              *string `json:"code,omitempty"`
	Name              *string `json:"name,omitempty"`
	CanCreateRequests bool    `json:"canCreateRequests"`
}

type ILLConfig struct {
	ISO18626URL                 *string     `json:"iso18626Url"`
	ISO18626Vendor              *string     `json:"iso18626Vendor"`
	LendersOfLastResort         []SymbolRef `json:"lendersOfLastResort"`
	IncludeRequestingAgencyInfo *bool       `json:"includeRequestingAgencyInfo"`
	IncludeSupplierInfo         *bool       `json:"includeSupplierInfo"`
	IncludeReturnInfo           *bool       `json:"includeReturnInfo"`
	IncludeVendorNote           *bool       `json:"includeVendorNote"`
	UseOfferedCosts             *bool       `json:"useOfferedCosts"`
	NoteFieldSeparator          *string     `json:"noteFieldSeparator"`
	SupplierPatronPattern       *string     `json:"supplierPatronPattern"`
	DuplicateCheckWindowHours   *int32      `json:"duplicateCheckWindowHours"`
}

type CatalogConfig struct {
	MetadataUpdateMode *string               `json:"metadataUpdateMode"`
	SRU                *SRUConfig            `json:"sru"`
	Zoom               *ZoomConfig           `json:"zoom"`
	Query              *QueryConfig          `json:"queryConfig"`
	HoldingsFormat     *HoldingsParserConfig `json:"holdingsFormat"`
	MetadataFormat     *MetadataParserConfig `json:"metadataFormat"`
}

type SRUConfig struct {
	Address      string  `json:"address"`
	RecordSchema *string `json:"recordSchema"`
}

type ZoomConfig struct {
	Address string             `json:"address"`
	Options *map[string]string `json:"options"`
}

type QueryConfig struct {
	Type       *string `json:"type"`
	Identifier *string `json:"identifier"`
	ISBN       *string `json:"isbn"`
	ISSN       *string `json:"issn"`
	Title      *string `json:"title"`
}

type HoldingsParserConfig struct {
	Marc        *MarcHoldingsParserConfig `json:"marc"`
	Marc21Plus1 *map[string]any           `json:"marc21plus1"`
	OPAC        *map[string]any           `json:"opac"`
	Reservoir   *map[string]any           `json:"reservoir"`
}

type MarcHoldingsParserConfig struct {
	CallNumberSubField       *string `json:"callNumberSubField"`
	ItemIDSubField           *string `json:"itemIdSubField"`
	LocationSubField         *string `json:"locationSubField"`
	MainField                *string `json:"mainField"`
	RestrictedSubField       *string `json:"restrictedSubField"`
	ShelvingLocationSubField *string `json:"shelvingLocationSubField"`
}

type MetadataParserConfig struct {
	Marc21 *MarcMetadataParserConfig `json:"marc21"`
}

type MarcMetadataParserConfig struct {
	Author     *string `json:"author"`
	Edition    *string `json:"edition"`
	Identifier *string `json:"identifier"`
	ISBN       *string `json:"isbn"`
	ISSN       *string `json:"issn"`
	Subtitle   *string `json:"subtitle"`
	Title      *string `json:"title"`
}

type HoldingsPolicy struct {
	Locations         []HoldingsLocation         `json:"locations"`
	ShelvingLocations []HoldingsShelvingLocation `json:"shelvingLocations"`
	LocationPolicies  []HoldingsLocationPolicy   `json:"locationPolicies"`
	ItemLoanPolicies  []HoldingsItemLoanPolicy   `json:"itemLoanPolicies"`
}

type HoldingsLocation struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	SupplyPreference int    `json:"supplyPreference"`
}

type HoldingsShelvingLocation = HoldingsLocation

type HoldingsLocationPolicy struct {
	LocationCode         *string `json:"locationCode"`
	ShelvingLocationCode string  `json:"shelvingLocationCode"`
	SupplyPreference     int     `json:"supplyPreference"`
}

type HoldingsItemLoanPolicy struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Lendable bool   `json:"lendable"`
}

func validateConfigEnums(catalog *CatalogConfig, ill *ILLConfig) error {
	if ill != nil && ill.ISO18626Vendor != nil && !oneOf(*ill.ISO18626Vendor, "Alma", "ReShare", "CrossLink", "ILLiad", "Unknown") {
		return fmt.Errorf("invalid ILL vendor: %s", *ill.ISO18626Vendor)
	}
	if ill != nil {
		for index := range ill.LendersOfLastResort {
			if err := ill.LendersOfLastResort[index].NormalizeAndValidate(); err != nil {
				return fmt.Errorf("lender of last resort %d: %w", index+1, err)
			}
		}
	}
	if catalog != nil && catalog.MetadataUpdateMode != nil && !oneOf(*catalog.MetadataUpdateMode, "replace", "merge", "none", "auto") {
		return fmt.Errorf("invalid metadata update mode: %s", *catalog.MetadataUpdateMode)
	}
	if catalog != nil && catalog.Query != nil && catalog.Query.Type != nil && !oneOf(*catalog.Query.Type, "cql", "pqf") {
		return fmt.Errorf("invalid query type: %s", *catalog.Query.Type)
	}
	return nil
}
