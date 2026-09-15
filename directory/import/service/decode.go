package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
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

func decodeRecord(data []byte, schemas map[string]*openapi3.Schema) (decodedRecord, error) {
	var record decodedRecord

	properties, err := objectProperties(data)
	if err != nil {
		return record, fmt.Errorf("invalid JSON record: %w", err)
	}
	if rawType, exists := properties["type"]; exists {
		_ = json.Unmarshal(rawType, &record.recordType)
	}
	schema, exists := schemas[record.recordType]
	if !exists {
		return record, fmt.Errorf("unknown import record type")
	}
	if err := validateAgainstSchema(data, schema); err != nil {
		return record, fmt.Errorf("invalid %s aggregate: %w", record.recordType, err)
	}

	var env envelope
	if err := decodeStrict(data, &env); err != nil {
		return record, fmt.Errorf("invalid import envelope: %w", err)
	}

	switch env.Type {
	case "entry":
		var aggregate model.EntryAggregate
		if err := decodeAggregate(env.Key, env.Data, &aggregate.Key, &aggregate.Data); err != nil {
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
		if err := decodeAggregate(env.Key, env.Data, &aggregate.Key, &aggregate.Data); err != nil {
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
		if err := decodeAggregate(env.Key, env.Data, &aggregate.Key, &aggregate.Data); err != nil {
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
		return record, fmt.Errorf("unknown import record type")
	}
	return record, nil
}

func validateAgainstSchema(data []byte, schema *openapi3.Schema) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return sanitizeJSONError(err)
	}
	if err := schema.VisitJSON(value,
		openapi3.EnableFormatValidation(),
		openapi3.SetSchemaErrorMessageCustomizer(func(err *openapi3.SchemaError) string {
			path := strings.Join(err.JSONPointer(), ".")
			if path == "" {
				path = "record"
			}
			reason := err.Reason
			if reason == "" {
				reason = "does not match the schema"
			}
			return path + ": " + reason
		}),
	); err != nil {
		return err
	}
	return nil
}

func decodeAggregate(keyJSON, dataJSON []byte, key, data any) error {
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
