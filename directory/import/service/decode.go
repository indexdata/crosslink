package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/indexdata/crosslink/directory/import/model"
)

type envelope struct {
	Type string          `json:"type"`
	Key  json.RawMessage `json:"key"`
	Data json.RawMessage `json:"data"`
}

type decodedRecord struct {
	recordType string
	key        string
	entry      *model.EntryAggregate
	tier       *model.TierAggregate
	network    *model.NetworkAggregate
}

var requiredProperties = map[string][]string{
	"envelope": {"type", "key", "data"},
	"entryKey": {"authority", "symbol"},
	"entryData": {
		"name", "type", "parent", "description", "organizationId", "contactName", "email", "fromEmail",
		"tenant", "vendor", "phoneNumber", "lmsLocationCode", "hrid", "timeZone", "symbols", "endpoints",
		"addresses", "closures", "lmsConfig", "catalogConfig", "illConfig", "holdingsPolicy",
	},
	"tierKey":     {"consortium", "name"},
	"tierData":    {"level", "type", "cost", "entries"},
	"networkKey":  {"consortium", "name"},
	"networkData": {"priority", "reciprocal", "entries"},
}

func decodeRecord(data []byte) (decodedRecord, error) {
	var record decodedRecord

	properties, err := objectProperties(data)
	if err != nil {
		return record, fmt.Errorf("invalid JSON record: %w", err)
	}
	if rawType, exists := properties["type"]; exists {
		_ = json.Unmarshal(rawType, &record.recordType)
	}
	if err := requireExactProperties(properties, requiredProperties["envelope"]); err != nil {
		return record, fmt.Errorf("invalid import envelope: %w", err)
	}

	var env envelope
	if err := decodeStrict(data, &env); err != nil {
		return record, fmt.Errorf("invalid import envelope: %w", err)
	}
	record.recordType = env.Type

	switch env.Type {
	case "entry":
		var aggregate model.EntryAggregate
		if err := validateEntryShape(env.Key, env.Data); err != nil {
			return record, fmt.Errorf("invalid entry aggregate: %w", err)
		}
		if err := decodeAggregate(env.Key, env.Data, requiredProperties["entryKey"], requiredProperties["entryData"], &aggregate.Key, &aggregate.Data); err != nil {
			return record, fmt.Errorf("invalid entry aggregate: %w", err)
		}
		record.entry = &aggregate
		validationErr := aggregate.NormalizeAndValidate()
		if aggregate.Key.Authority != "" && aggregate.Key.Symbol != "" {
			record.key = aggregate.Key.String()
		}
		if validationErr != nil {
			return record, validationErr
		}
	case "tier":
		var aggregate model.TierAggregate
		if err := validateMembershipShape(env.Key, env.Data, "tier"); err != nil {
			return record, fmt.Errorf("invalid tier aggregate: %w", err)
		}
		if err := decodeAggregate(env.Key, env.Data, requiredProperties["tierKey"], requiredProperties["tierData"], &aggregate.Key, &aggregate.Data); err != nil {
			return record, fmt.Errorf("invalid tier aggregate: %w", err)
		}
		record.tier = &aggregate
		validationErr := aggregate.NormalizeAndValidate()
		if aggregate.Key.Consortium.Authority != "" && aggregate.Key.Consortium.Symbol != "" && aggregate.Key.Name != "" {
			record.key = aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
		}
		if validationErr != nil {
			return record, validationErr
		}
	case "network":
		var aggregate model.NetworkAggregate
		if err := validateMembershipShape(env.Key, env.Data, "network"); err != nil {
			return record, fmt.Errorf("invalid network aggregate: %w", err)
		}
		if err := decodeAggregate(env.Key, env.Data, requiredProperties["networkKey"], requiredProperties["networkData"], &aggregate.Key, &aggregate.Data); err != nil {
			return record, fmt.Errorf("invalid network aggregate: %w", err)
		}
		record.network = &aggregate
		validationErr := aggregate.NormalizeAndValidate()
		if aggregate.Key.Consortium.Authority != "" && aggregate.Key.Consortium.Symbol != "" && aggregate.Key.Name != "" {
			record.key = aggregate.Key.Consortium.String() + "/" + aggregate.Key.Name
		}
		if validationErr != nil {
			return record, validationErr
		}
	default:
		return record, fmt.Errorf("unknown import record type: %s", env.Type)
	}
	return record, nil
}

func validateEntryShape(keyJSON, dataJSON []byte) error {
	if err := validateObject(keyJSON, requiredProperties["entryKey"], nil); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	return validateObject(dataJSON, requiredProperties["entryData"], func(properties map[string]json.RawMessage) error {
		if err := validateNullableObject(properties["parent"], []string{"authority", "symbol"}, nil); err != nil {
			return fmt.Errorf("parent: %w", err)
		}
		if err := validateObjectArray(properties["symbols"], []string{"authority", "symbol"}, nil); err != nil {
			return fmt.Errorf("symbols: %w", err)
		}
		if err := validateObjectArray(properties["endpoints"], []string{"name", "type", "address"}, nil); err != nil {
			return fmt.Errorf("endpoints: %w", err)
		}
		if err := validateObjectArray(properties["addresses"], []string{"type", "addressComponents"}, func(address map[string]json.RawMessage) error {
			return validateObjectArray(address["addressComponents"], []string{"seq", "type", "value"}, nil)
		}); err != nil {
			return fmt.Errorf("addresses: %w", err)
		}
		if err := validateObjectArray(properties["closures"], []string{"startDate", "endDate", "reason"}, nil); err != nil {
			return fmt.Errorf("closures: %w", err)
		}
		if err := validateNullableObject(properties["lmsConfig"], []string{
			"address", "fromAgency", "fromAgencyAuthentication", "toAgency", "lookupUserEnabled", "acceptItemEnabled",
			"checkInItemEnabled", "checkOutItemEnabled", "itemLocation", "requestItemRequestType", "requestItemRequestScopeType",
			"requestItemBibIdCode", "requestItemEnabled", "requestItemPickupLocationEnabled", "requesterPickupLocation",
			"supplierPickupLocation", "requesterPatronPattern",
		}, nil); err != nil {
			return fmt.Errorf("lmsConfig: %w", err)
		}
		if err := validateCatalogShape(properties["catalogConfig"]); err != nil {
			return fmt.Errorf("catalogConfig: %w", err)
		}
		if err := validateNullableObject(properties["illConfig"], []string{
			"iso18626Url", "iso18626Vendor", "lendersOfLastResort", "includeRequestingAgencyInfo", "includeSupplierInfo",
			"includeReturnInfo", "includeVendorNote", "useOfferedCosts", "noteFieldSeparator", "supplierPatronPattern",
			"duplicateCheckWindowHours",
		}, func(config map[string]json.RawMessage) error {
			return validateObjectArray(config["lendersOfLastResort"], []string{"authority", "symbol"}, nil)
		}); err != nil {
			return fmt.Errorf("illConfig: %w", err)
		}
		if err := validateHoldingsPolicyShape(properties["holdingsPolicy"]); err != nil {
			return fmt.Errorf("holdingsPolicy: %w", err)
		}
		return nil
	})
}

func validateMembershipShape(keyJSON, dataJSON []byte, aggregateType string) error {
	keyRequired := requiredProperties[aggregateType+"Key"]
	dataRequired := requiredProperties[aggregateType+"Data"]
	if err := validateObject(keyJSON, keyRequired, func(key map[string]json.RawMessage) error {
		return validateObject(key["consortium"], []string{"authority", "symbol"}, nil)
	}); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	if err := validateObject(dataJSON, dataRequired, func(data map[string]json.RawMessage) error {
		return validateObjectArray(data["entries"], []string{"authority", "symbol"}, nil)
	}); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

func validateCatalogShape(raw json.RawMessage) error {
	return validateNullableObject(raw, []string{"metadataUpdateMode", "sru", "zoom", "queryConfig", "holdingsFormat", "metadataFormat"}, func(config map[string]json.RawMessage) error {
		checks := []struct {
			name       string
			properties []string
			nested     func(map[string]json.RawMessage) error
		}{
			{"sru", []string{"address", "recordSchema"}, nil},
			{"zoom", []string{"address", "options"}, nil},
			{"queryConfig", []string{"type", "identifier", "isbn", "issn", "title"}, nil},
			{"holdingsFormat", []string{"marc", "marc21plus1", "opac", "reservoir"}, func(format map[string]json.RawMessage) error {
				return validateNullableObject(format["marc"], []string{"callNumberSubField", "itemIdSubField", "locationSubField", "mainField", "restrictedSubField", "shelvingLocationSubField"}, nil)
			}},
			{"metadataFormat", []string{"marc21"}, func(format map[string]json.RawMessage) error {
				return validateNullableObject(format["marc21"], []string{"author", "edition", "identifier", "isbn", "issn", "subtitle", "title"}, nil)
			}},
		}
		for _, check := range checks {
			if err := validateNullableObject(config[check.name], check.properties, check.nested); err != nil {
				return fmt.Errorf("%s: %w", check.name, err)
			}
		}
		return nil
	})
}

func validateHoldingsPolicyShape(raw json.RawMessage) error {
	return validateNullableObject(raw, []string{"locations", "shelvingLocations", "locationPolicies", "itemLoanPolicies"}, func(policy map[string]json.RawMessage) error {
		if err := validateObjectArray(policy["locations"], []string{"code", "name", "supplyPreference"}, nil); err != nil {
			return fmt.Errorf("locations: %w", err)
		}
		if err := validateObjectArray(policy["shelvingLocations"], []string{"code", "name", "supplyPreference"}, nil); err != nil {
			return fmt.Errorf("shelvingLocations: %w", err)
		}
		if err := validateObjectArray(policy["locationPolicies"], []string{"locationCode", "shelvingLocationCode", "supplyPreference"}, nil); err != nil {
			return fmt.Errorf("locationPolicies: %w", err)
		}
		if err := validateObjectArray(policy["itemLoanPolicies"], []string{"code", "name", "lendable"}, nil); err != nil {
			return fmt.Errorf("itemLoanPolicies: %w", err)
		}
		return nil
	})
}

func validateNullableObject(raw json.RawMessage, required []string, nested func(map[string]json.RawMessage) error) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return validateObject(raw, required, nested)
}

func validateObject(raw json.RawMessage, required []string, nested func(map[string]json.RawMessage) error) error {
	properties, err := objectProperties(raw)
	if err != nil {
		return err
	}
	if err := requireExactProperties(properties, required); err != nil {
		return err
	}
	if nested != nil {
		return nested(properties)
	}
	return nil
}

func validateObjectArray(raw json.RawMessage, required []string, nested func(map[string]json.RawMessage) error) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be an array, not null")
	}
	var values []json.RawMessage
	if err := decodeStrict(raw, &values); err != nil {
		return err
	}
	if values == nil {
		return fmt.Errorf("must be an array")
	}
	for index, value := range values {
		if err := validateObject(value, required, nested); err != nil {
			return fmt.Errorf("item %d: %w", index+1, err)
		}
	}
	return nil
}

func decodeAggregate(keyJSON, dataJSON []byte, keyProperties, dataProperties []string, key, data any) error {
	keyObject, err := objectProperties(keyJSON)
	if err != nil {
		return fmt.Errorf("key must be an object: %w", err)
	}
	if err := requireExactProperties(keyObject, keyProperties); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	dataObject, err := objectProperties(dataJSON)
	if err != nil {
		return fmt.Errorf("data must be an object: %w", err)
	}
	if err := requireExactProperties(dataObject, dataProperties); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if err := decodeStrict(keyJSON, key); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	if err := decodeStrict(dataJSON, data); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

func objectProperties(data []byte) (map[string]json.RawMessage, error) {
	var properties map[string]json.RawMessage
	if err := decodeStrict(data, &properties); err != nil {
		return nil, err
	}
	if properties == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return properties, nil
}

func requireExactProperties(properties map[string]json.RawMessage, required []string) error {
	allowed := make(map[string]struct{}, len(required))
	for _, property := range required {
		allowed[property] = struct{}{}
		if _, exists := properties[property]; !exists {
			return fmt.Errorf("missing required property %q", property)
		}
	}
	unknown := make([]string, 0)
	for property := range properties {
		if _, exists := allowed[property]; !exists {
			unknown = append(unknown, property)
		}
	}
	if len(unknown) != 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown property %q", unknown[0])
	}
	return nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return sanitizeJSONError(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return sanitizeJSONError(err)
	}
	return nil
}

func sanitizeJSONError(err error) error {
	message := err.Error()
	if index := strings.Index(message, " looking for beginning of value"); index >= 0 {
		return fmt.Errorf("invalid JSON syntax%s", message[index:])
	}
	return err
}
