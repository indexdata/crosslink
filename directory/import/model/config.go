package model

import (
	"fmt"
	"strings"
)

type LMSConfig struct {
	Vendor                           *string          `json:"vendor"`
	NcipNamespaceEnabled             *bool            `json:"ncipNamespaceEnabled"`
	BibIDNormalization               *string          `json:"bibIdNormalization"`
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
	Profile            *string               `json:"profile"`
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
	Marc        *MarcHoldingsParserConfig `json:"marc,omitempty"`
	Marc21Plus1 *map[string]any           `json:"marc21plus1,omitempty"`
	OPAC        *OpacHoldingsParserConfig `json:"opac,omitempty"`
	Reservoir   *map[string]any           `json:"reservoir,omitempty"`
}

type MarcHoldingsParserConfig struct {
	Availability             *[]MarcAvailabilityPredicate `json:"availability,omitempty"`
	CallNumberSubField       *string                      `json:"callNumberSubField,omitempty"`
	ItemIDSubField           *string                      `json:"itemIdSubField,omitempty"`
	LocationSubField         *string                      `json:"locationSubField,omitempty"`
	MainField                *string                      `json:"mainField,omitempty"`
	RestrictedSubField       *string                      `json:"restrictedSubField,omitempty"`
	ShelvingLocationSubField *string                      `json:"shelvingLocationSubField,omitempty"`
}

type MarcAvailabilityPredicate struct {
	SubField string  `json:"subField"`
	Operator string  `json:"operator"`
	Value    *string `json:"value,omitempty"`
}

type OpacHoldingsParserConfig struct {
	AvailabilityRule         *string   `json:"availabilityRule,omitempty"`
	AvailablePublicNotes     *[]string `json:"availablePublicNotes,omitempty"`
	RequireLocalLocation     *bool     `json:"requireLocalLocation,omitempty"`
	ShelvingLocationSource   *string   `json:"shelvingLocationSource,omitempty"`
	IncludeItemID            *bool     `json:"includeItemId,omitempty"`
	IncludeItemLoanPolicy    *bool     `json:"includeItemLoanPolicy,omitempty"`
	IncludeTemporaryLocation *bool     `json:"includeTemporaryLocation,omitempty"`
	AllCirculations          *bool     `json:"allCirculations,omitempty"`
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

func validateConfigEnums(lms *LMSConfig, catalog *CatalogConfig, ill *ILLConfig) error {
	if lms != nil {
		if lms.Vendor != nil && !oneOf(*lms.Vendor, "Generic", "Alma", "Sierra", "Koha", "FOLIO", "WMS", "Aleph") {
			return fmt.Errorf("invalid lmsConfig.vendor")
		}
		if lms.BibIDNormalization != nil && !oneOf(*lms.BibIDNormalization, "none", "sierra") {
			return fmt.Errorf("invalid lmsConfig.bibIdNormalization")
		}
	}
	if catalog != nil {
		if catalog.Profile != nil && !oneOf(*catalog.Profile, "Generic", "Alma", "Sierra", "Koha", "FOLIO", "WMS", "Aleph") {
			return fmt.Errorf("invalid catalogConfig.profile")
		}
		if catalog.SRU != nil && catalog.Zoom != nil {
			return fmt.Errorf("catalogConfig cannot configure both SRU and ZOOM endpoints")
		}
		if h := catalog.HoldingsFormat; h != nil {
			count := 0
			for _, present := range []bool{h.Marc != nil, h.OPAC != nil, h.Reservoir != nil, h.Marc21Plus1 != nil} {
				if present {
					count++
				}
			}
			if count > 1 {
				return fmt.Errorf("catalogConfig.holdingsFormat must set at most one of marc, opac, reservoir, or marc21plus1")
			}
			if h.Marc != nil && h.Marc.Availability != nil {
				for _, predicate := range *h.Marc.Availability {
					if predicate.SubField == "" || !oneOf(predicate.Operator, "equals", "absent") || (predicate.Operator == "equals" && predicate.Value == nil) {
						return fmt.Errorf("invalid catalogConfig.holdingsFormat.marc.availability predicate")
					}
				}
			}
			if h.OPAC != nil {
				if h.OPAC.AvailabilityRule != nil && !oneOf(*h.OPAC.AvailabilityRule, "availableNow", "publicNote") {
					return fmt.Errorf("invalid catalogConfig.holdingsFormat.opac.availabilityRule")
				}
				if h.OPAC.ShelvingLocationSource != nil && !oneOf(*h.OPAC.ShelvingLocationSource, "shelvingLocation", "localLocation") {
					return fmt.Errorf("invalid catalogConfig.holdingsFormat.opac.shelvingLocationSource")
				}
			}
		}
	}
	if ill != nil && ill.ISO18626Vendor != nil && !oneOf(*ill.ISO18626Vendor, "Alma", "ReShare", "CrossLink", "ILLiad", "Unknown") {
		return fmt.Errorf("invalid ILL vendor: %s", *ill.ISO18626Vendor)
	}
	if ill != nil {
		for index := range ill.LendersOfLastResort {
			if err := ill.LendersOfLastResort[index].NormalizeAndValidate(); err != nil {
				return fmt.Errorf("lender of last resort %d: %w", index+1, err)
			}
			if strings.Contains(ill.LendersOfLastResort[index].Authority, ":") {
				return fmt.Errorf("lender of last resort %d authority must not contain ':'", index+1)
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
