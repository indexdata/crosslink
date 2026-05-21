package api

import (
	"context"
	"errors"
	"indexdata/directory/auth"
	"indexdata/directory/db"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddEntryTier(ctx context.Context, request AddEntryTierRequestObject) (AddEntryTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddEntryTier401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddEntryTier")
		return AddEntryTier500TextResponse("Internal server error"), nil
	}

	if request.Body == nil || request.Body.Entry == uuid.Nil || request.Body.Tier == uuid.Nil {
		return AddEntryTier400TextResponse("You most provide a valid entry and tier"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedEntryTier, err := qtx.CreateEntryTier(ctx, db.CreateEntryTierParams{
		Entry: request.Body.Entry,
		Tier:  request.Body.Tier,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create entry tier", "error", err, "entry", request.Body.Entry, "tier", request.Body.Tier)
		return AddEntryTier500TextResponse("Error creating entry tier"), nil
	}

	var resp Id
	resp.Id = insertedEntryTier.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddEntryTier")
		return AddEntryTier500TextResponse("Internal server error"), nil
	}

	return AddEntryTier201JSONResponse(resp), nil

}

func (a ApiImpl) GetEntryTierByID(ctx context.Context, request GetEntryTierByIDRequestObject) (GetEntryTierByIDResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntryTierByID401TextResponse("Access denied"), nil
	}

	entryTier, err := a.queries.GetEntryTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetEntryTierByID404TextResponse("Entry Tier not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get entry tier", "error", err, "id", request.Id)
			return GetEntryTierByID500TextResponse("Internal server error"), nil
		}
	}

	entryTierResponse := EntryTier{
		Id:    &entryTier.ID,
		Entry: entryTier.Entry,
		Tier:  entryTier.Tier,
	}

	return GetEntryTierByID200JSONResponse(entryTierResponse), nil
}

func (a ApiImpl) GetEntryTiers(ctx context.Context, request GetEntryTiersRequestObject) (GetEntryTiersResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntryTiers401TextResponse("Access denied"), nil
	}

	queryField, queryValue, err := SplitQuery(request.Params.Q)

	if err != nil {
		return GetEntryTiers400TextResponse("Malformed query string"), nil
	}

	params := db.ListEntryTiersParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
		Field:  queryField,
		Value:  queryValue,
	}

	rows, err := a.queries.ListEntryTiers(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list entry tiers", "error", err)
		return GetEntryTiers500TextResponse("Internal Server Error"), nil
	}

	entryTierList := make([]EntryTier, 0, len(rows))

	for _, row := range rows {
		entryTier := EntryTier{
			Id:    &row.ID,
			Entry: row.Entry,
			Tier:  row.Tier,
		}
		entryTierList = append(entryTierList, entryTier)
	}

	resp := EntryTiersResponse{
		Items: entryTierList,
		About: About{
			Count: int64(len(entryTierList)),
		},
	}

	return GetEntryTiers200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteEntryTier(ctx context.Context, request DeleteEntryTierRequestObject) (DeleteEntryTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteEntryTier401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteEntryTierById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete entry tier", "error", err, "id", request.Id)
		return DeleteEntryTier500TextResponse("Internal server error"), nil
	}
	return DeleteEntryTier204Response{}, nil

}
