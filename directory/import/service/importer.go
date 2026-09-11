package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/indexdata/crosslink/directory/import/model"
)

const (
	maxRecordBytes             = 1 << 20
	maxRetainedErrorDetails    = 1000
	maxRetainedErrorFieldBytes = 1024
)

var ErrRecordTooLarge = errors.New("import record exceeds 1 MiB limit")

type Repository interface {
	ImportEntry(context.Context, model.EntryAggregate, model.ConflictPolicy) (model.RepoResult, error)
	ImportTier(context.Context, model.TierAggregate, model.ConflictPolicy) (model.RepoResult, error)
	ImportNetwork(context.Context, model.NetworkAggregate, model.ConflictPolicy) (model.RepoResult, error)
}

type Importer struct {
	repository     Repository
	recordSchemas  map[string]*openapi3.Schema
	maxRecordBytes int
}

func New(repository Repository, spec *openapi3.T) (*Importer, error) {
	schemas := make(map[string]*openapi3.Schema, 3)
	for _, recordSchema := range []struct {
		recordType    string
		componentName string
	}{
		{recordType: "entry", componentName: "ImportEntryRecord"},
		{recordType: "tier", componentName: "ImportTierRecord"},
		{recordType: "network", componentName: "ImportNetworkRecord"},
	} {
		var schemaRef *openapi3.SchemaRef
		if spec != nil && spec.Components != nil {
			schemaRef = spec.Components.Schemas[recordSchema.componentName]
		}
		if schemaRef == nil || schemaRef.Value == nil {
			return nil, fmt.Errorf("OpenAPI component schema %q is missing", recordSchema.componentName)
		}
		schemas[recordSchema.recordType] = schemaRef.Value
	}
	return &Importer{repository: repository, recordSchemas: schemas, maxRecordBytes: maxRecordBytes}, nil
}

func (i *Importer) Import(ctx context.Context, policy model.ConflictPolicy, input io.Reader) (model.ImportResult, error) {
	result := model.ImportResult{Errors: make([]model.ImportItemError, 0)}
	reader := bufio.NewReaderSize(input, i.maxRecordBytes+2)
	var recordNumber int32

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line, readErr := reader.ReadSlice('\n')
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			return result, ErrRecordTooLarge
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return result, fmt.Errorf("read import stream: %w", readErr)
		}

		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) > i.maxRecordBytes {
			return result, ErrRecordTooLarge
		}
		if len(bytes.TrimSpace(line)) != 0 {
			recordNumber++
			if err := i.importRecord(ctx, policy, recordNumber, line, &result); err != nil {
				return result, err
			}
		}

		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
	}
}

func (i *Importer) importRecord(ctx context.Context, policy model.ConflictPolicy, line int32, data []byte, result *model.ImportResult) error {
	record, err := decodeRecord(data, i.recordSchemas)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		incrementFailed(result, record.recordType)
		appendError(result, line, record.recordType, record.key, err.Error())
		return nil
	}

	var repoResult model.RepoResult
	switch record.recordType {
	case "entry":
		repoResult, err = i.repository.ImportEntry(ctx, *record.entry, policy)
	case "tier":
		repoResult, err = i.repository.ImportTier(ctx, *record.tier, policy)
	case "network":
		repoResult, err = i.repository.ImportNetwork(ctx, *record.network, policy)
	}
	if err != nil {
		if isContextError(err) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		incrementFailed(result, record.recordType)
		appendError(result, line, record.recordType, record.key, err.Error())
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	switch repoResult.Outcome {
	case model.OutcomeImported:
		section(result, record.recordType).Imported++
	case model.OutcomeSkipped:
		section(result, record.recordType).Skipped++
		diagnostic := repoResult.Diagnostic
		if diagnostic == "" {
			diagnostic = "record skipped because its business key already exists"
		}
		appendError(result, line, record.recordType, record.key, diagnostic)
	default:
		incrementFailed(result, record.recordType)
		appendError(result, line, record.recordType, record.key, "repository returned an invalid import outcome")
	}
	return nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func incrementFailed(result *model.ImportResult, recordType string) {
	if target := section(result, recordType); target != nil {
		target.Failed++
	}
}

func section(result *model.ImportResult, recordType string) *model.ImportSectionResult {
	switch recordType {
	case "entry":
		return &result.Entries
	case "tier":
		return &result.Tiers
	case "network":
		return &result.Networks
	default:
		return nil
	}
}

func appendError(result *model.ImportResult, line int32, recordType, key, message string) {
	if len(result.Errors) >= maxRetainedErrorDetails {
		result.ErrorsOmitted++
		return
	}
	item := model.ImportItemError{Line: line, Error: truncateErrorField(message)}
	switch recordType {
	case "entry", "tier", "network":
		item.Type = &recordType
	}
	if key != "" {
		key = truncateErrorField(key)
		item.Key = &key
	}
	result.Errors = append(result.Errors, item)
}

func truncateErrorField(value string) string {
	if len(value) <= maxRetainedErrorFieldBytes {
		return value
	}
	const suffix = "..."
	end := maxRetainedErrorFieldBytes - len(suffix)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}
