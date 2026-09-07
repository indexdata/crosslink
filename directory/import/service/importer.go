package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/indexdata/crosslink/directory/import/model"
)

const maxRecordBytes = 1 << 20

var ErrRecordTooLarge = errors.New("import record exceeds 1 MiB limit")

type Repository interface {
	ImportEntry(context.Context, model.EntryAggregate, model.ConflictPolicy) (model.RepoResult, error)
	ImportTier(context.Context, model.TierAggregate, model.ConflictPolicy) (model.RepoResult, error)
	ImportNetwork(context.Context, model.NetworkAggregate, model.ConflictPolicy) (model.RepoResult, error)
}

type Importer struct {
	repository     Repository
	maxRecordBytes int
}

func New(repository Repository) *Importer {
	return &Importer{repository: repository, maxRecordBytes: maxRecordBytes}
}

func (i *Importer) Import(ctx context.Context, policy model.ConflictPolicy, input io.Reader) (model.ImportResult, error) {
	result := model.ImportResult{Errors: make([]model.ImportItemError, 0)}
	reader := bufio.NewReaderSize(input, i.maxRecordBytes+2)
	var recordNumber int32

	for {
		line, readErr := reader.ReadSlice('\n')
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
			i.importRecord(ctx, policy, recordNumber, line, &result)
		}

		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
	}
}

func (i *Importer) importRecord(ctx context.Context, policy model.ConflictPolicy, line int32, data []byte, result *model.ImportResult) {
	record, err := decodeRecord(data)
	if err != nil {
		incrementFailed(result, record.recordType)
		appendError(result, line, record.recordType, record.key, err.Error())
		return
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
		incrementFailed(result, record.recordType)
		appendError(result, line, record.recordType, record.key, err.Error())
		return
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
	item := model.ImportItemError{Line: line, Error: message}
	if recordType != "" {
		item.Type = &recordType
	}
	if key != "" {
		item.Key = &key
	}
	result.Errors = append(result.Errors, item)
}
