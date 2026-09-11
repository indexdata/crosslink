package model

import (
	"fmt"
	"strings"
	"time"
)

type ConflictPolicy string

const (
	ConflictPolicyFail   ConflictPolicy = "fail"
	ConflictPolicySkip   ConflictPolicy = "skip"
	ConflictPolicyUpdate ConflictPolicy = "update"
)

func ParseConflictPolicy(value string) (ConflictPolicy, error) {
	switch ConflictPolicy(value) {
	case "", ConflictPolicyFail:
		return ConflictPolicyFail, nil
	case ConflictPolicySkip:
		return ConflictPolicySkip, nil
	case ConflictPolicyUpdate:
		return ConflictPolicyUpdate, nil
	default:
		return "", fmt.Errorf("unknown conflict policy: %s", value)
	}
}

type Outcome string

const (
	OutcomeImported Outcome = "imported"
	OutcomeSkipped  Outcome = "skipped"
)

type RepoResult struct {
	Outcome    Outcome
	Diagnostic string
}

type ImportSectionResult struct {
	Imported int32 `json:"imported"`
	Failed   int32 `json:"failed"`
	Skipped  int32 `json:"skipped"`
}

type ImportItemError struct {
	Line  int32   `json:"line"`
	Type  *string `json:"type,omitempty"`
	Key   *string `json:"key,omitempty"`
	Error string  `json:"error"`
}

type ImportResult struct {
	Entries       ImportSectionResult `json:"entries"`
	Tiers         ImportSectionResult `json:"tiers"`
	Networks      ImportSectionResult `json:"networks"`
	Errors        []ImportItemError   `json:"errors"`
	ErrorsOmitted int32               `json:"errorsOmitted"`
}

type SymbolRef struct {
	Authority string `json:"authority"`
	Symbol    string `json:"symbol"`
}

func (s *SymbolRef) NormalizeAndValidate() error {
	s.Authority = strings.ToUpper(strings.TrimSpace(s.Authority))
	s.Symbol = strings.ToUpper(strings.TrimSpace(s.Symbol))
	if s.Authority == "" || s.Symbol == "" {
		return fmt.Errorf("symbol authority and symbol are required")
	}
	return nil
}

func (s SymbolRef) String() string { return s.Authority + ":" + s.Symbol }

type EntryAggregate struct {
	Key  SymbolRef
	Data EntryData
}

type EntryData struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Parent          *SymbolRef        `json:"parent"`
	Description     *string           `json:"description"`
	OrganizationID  *string           `json:"organizationId"`
	ContactName     *string           `json:"contactName"`
	Email           *string           `json:"email"`
	FromEmail       *string           `json:"fromEmail"`
	Tenant          *string           `json:"tenant"`
	Vendor          *string           `json:"vendor"`
	PhoneNumber     *string           `json:"phoneNumber"`
	LMSLocationCode *string           `json:"lmsLocationCode"`
	HRID            *string           `json:"hrid"`
	TimeZone        *string           `json:"timeZone"`
	Symbols         []SymbolRef       `json:"symbols"`
	Endpoints       []ServiceEndpoint `json:"endpoints"`
	Addresses       []Address         `json:"addresses"`
	Closures        []Closure         `json:"closures"`
	LMSConfig       *LMSConfig        `json:"lmsConfig"`
	CatalogConfig   *CatalogConfig    `json:"catalogConfig"`
	ILLConfig       *ILLConfig        `json:"illConfig"`
	HoldingsPolicy  *HoldingsPolicy   `json:"holdingsPolicy"`
}

type ServiceEndpoint struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Address string `json:"address"`
}

type Address struct {
	Type       string             `json:"type"`
	Components []AddressComponent `json:"addressComponents"`
}

type AddressComponent struct {
	Seq   int32  `json:"seq"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Closure struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Reason    string `json:"reason"`
}

func (a *EntryAggregate) NormalizeAndValidate() error {
	if err := a.Key.NormalizeAndValidate(); err != nil {
		return fmt.Errorf("entry key: %w", err)
	}
	if strings.TrimSpace(a.Data.Name) == "" {
		return fmt.Errorf("entry name is required")
	}
	if !oneOf(a.Data.Type, "Institution", "Consortium", "Branch") {
		return fmt.Errorf("invalid entry type: %s", a.Data.Type)
	}
	if a.Data.Vendor != nil && !oneOf(*a.Data.Vendor, "Alma", "ReShare", "CrossLink", "ILLiad", "Unknown") {
		return fmt.Errorf("invalid entry vendor: %s", *a.Data.Vendor)
	}
	if a.Data.Parent != nil {
		if err := a.Data.Parent.NormalizeAndValidate(); err != nil {
			return fmt.Errorf("parent: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(a.Data.Symbols))
	keyCount := 0
	for index := range a.Data.Symbols {
		if err := a.Data.Symbols[index].NormalizeAndValidate(); err != nil {
			return fmt.Errorf("symbol %d: %w", index+1, err)
		}
		value := a.Data.Symbols[index].String()
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate entry symbol %s", value)
		}
		seen[value] = struct{}{}
		if value == a.Key.String() {
			keyCount++
		}
	}
	if keyCount != 1 {
		return fmt.Errorf("entry key %s must appear exactly once in symbols", a.Key.String())
	}
	for _, address := range a.Data.Addresses {
		if !oneOf(address.Type, "Default", "Shipping", "Billing", "Other") {
			return fmt.Errorf("invalid address type: %s", address.Type)
		}
		for _, component := range address.Components {
			if !oneOf(component.Type, "Thoroughfare", "Locality", "AdministrativeArea", "PostalCode", "CountryCode", "Other") {
				return fmt.Errorf("invalid address component type: %s", component.Type)
			}
		}
	}
	for index, closure := range a.Data.Closures {
		start, err := time.Parse(time.DateOnly, closure.StartDate)
		if err != nil {
			return fmt.Errorf("closure %d has invalid startDate", index+1)
		}
		end, err := time.Parse(time.DateOnly, closure.EndDate)
		if err != nil {
			return fmt.Errorf("closure %d has invalid endDate", index+1)
		}
		if end.Before(start) {
			return fmt.Errorf("closure %d endDate must not precede startDate", index+1)
		}
	}
	return validateConfigEnums(a.Data.CatalogConfig, a.Data.ILLConfig)
}

type TierKey struct {
	Consortium SymbolRef `json:"consortium"`
	Name       string    `json:"name"`
}

type TierData struct {
	Level   string      `json:"level"`
	Type    string      `json:"type"`
	Cost    float64     `json:"cost"`
	Entries []SymbolRef `json:"entries"`
}

type TierAggregate struct {
	Key  TierKey
	Data TierData
}

func (a *TierAggregate) NormalizeAndValidate() error {
	if err := a.Key.Consortium.NormalizeAndValidate(); err != nil {
		return fmt.Errorf("tier consortium: %w", err)
	}
	if strings.TrimSpace(a.Key.Name) == "" {
		return fmt.Errorf("tier name is required")
	}
	if !oneOf(a.Data.Level, "express", "normal", "rush", "secondarymail", "standard", "urgent") {
		return fmt.Errorf("invalid tier level: %s", a.Data.Level)
	}
	if !oneOf(a.Data.Type, "loan", "copy") {
		return fmt.Errorf("invalid tier type: %s", a.Data.Type)
	}
	return normalizeUniqueRefs(a.Data.Entries, "tier", func(refs []SymbolRef) { a.Data.Entries = refs })
}

type NetworkKey struct {
	Consortium SymbolRef `json:"consortium"`
	Name       string    `json:"name"`
}

type NetworkAssignment struct {
	SymbolRef
	Priority int32 `json:"priority"`
}

type NetworkData struct {
	Reciprocal *bool               `json:"reciprocal"`
	Entries    []NetworkAssignment `json:"entries"`
}

type NetworkAggregate struct {
	Key  NetworkKey
	Data NetworkData
}

func (a *NetworkAggregate) NormalizeAndValidate() error {
	if err := a.Key.Consortium.NormalizeAndValidate(); err != nil {
		return fmt.Errorf("network consortium: %w", err)
	}
	if strings.TrimSpace(a.Key.Name) == "" {
		return fmt.Errorf("network name is required")
	}
	seen := make(map[string]struct{}, len(a.Data.Entries))
	for index := range a.Data.Entries {
		if err := a.Data.Entries[index].NormalizeAndValidate(); err != nil {
			return fmt.Errorf("network entry %d: %w", index+1, err)
		}
		key := a.Data.Entries[index].String()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate network entry %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func normalizeUniqueRefs(refs []SymbolRef, resource string, assign func([]SymbolRef)) error {
	seen := make(map[string]struct{}, len(refs))
	for index := range refs {
		if err := refs[index].NormalizeAndValidate(); err != nil {
			return fmt.Errorf("%s entry %d: %w", resource, index+1, err)
		}
		key := refs[index].String()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate %s entry %s", resource, key)
		}
		seen[key] = struct{}{}
	}
	assign(refs)
	return nil
}

func oneOf(value string, valid ...string) bool {
	for _, candidate := range valid {
		if value == candidate {
			return true
		}
	}
	return false
}
