package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/import/model"
	importservice "github.com/indexdata/crosslink/directory/import/service"
)

func (a ApiImpl) PostImport(ctx context.Context, request PostImportRequestObject) (PostImportResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	if authData == nil || !authData.HasRole(auth.ConsortialAdminRole) {
		return PostImport401TextResponse("Access denied"), nil
	}

	policyValue := ""
	if request.Params.ConflictPolicy != nil {
		policyValue = string(*request.Params.ConflictPolicy)
	}
	policy, err := model.ParseConflictPolicy(policyValue)
	if err != nil {
		return PostImport400TextResponse(err.Error()), nil
	}
	if request.Body == nil || request.Body == http.NoBody {
		return PostImport400TextResponse("body is required"), nil
	}
	if a.importer == nil {
		return PostImport500TextResponse("import service is unavailable"), nil
	}

	result, err := a.importer.Import(ctx, policy, request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.Is(err, importservice.ErrRecordTooLarge) || errors.As(err, &maxBytesError) {
			return PostImport413TextResponse("import request too large"), nil
		}
		return PostImport500TextResponse("failed to read import request"), nil
	}
	return PostImport200JSONResponse(mapImportResult(result)), nil
}

func mapImportResult(result model.ImportResult) ImportResult {
	errors := make([]ImportItemError, 0, len(result.Errors))
	for _, source := range result.Errors {
		item := ImportItemError{Line: source.Line, Key: source.Key, Error: source.Error}
		if source.Type != nil {
			value := ImportItemType(*source.Type)
			item.Type = &value
		}
		errors = append(errors, item)
	}
	return ImportResult{
		Entries:  mapImportSection(result.Entries),
		Tiers:    mapImportSection(result.Tiers),
		Networks: mapImportSection(result.Networks),
		Errors:   errors,
	}
}

func mapImportSection(section model.ImportSectionResult) ImportSectionResult {
	return ImportSectionResult{Imported: section.Imported, Failed: section.Failed, Skipped: section.Skipped}
}
