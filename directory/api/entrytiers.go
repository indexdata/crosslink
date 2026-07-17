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

func (a ApiImpl) AddTierForEntry(ctx context.Context, request AddTierForEntryRequestObject) (AddTierForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddTierForEntry401TextResponse("Access denied"), nil
	}

	if request.Body == nil || request.Body.Id == uuid.Nil {
		return AddTierForEntry400TextResponse("You must provide a valid tier to add"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddTierForEntry")
		return AddTierForEntry500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	//Find the Entry

	qtx := a.queries.WithTx(tx)

	var orig db.Entry

	if request.Key == AddTierForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return AddTierForEntry400TextResponse("Error parsing id"), nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	} else if request.Key == AddTierForEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return AddTierForEntry400TextResponse("Unable to parse symbol"), nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return AddTierForEntry404TextResponse("Entry not found"), nil
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch entry", "error", err)
		return AddTierForEntry500TextResponse("Internal server error"), nil
	}

	insertedTierForEntry, err := qtx.CreateEntryTier(ctx, db.CreateEntryTierParams{
		Entry: orig.ID,
		Tier:  request.Body.Id,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to add tier to entry", "error", err, "entry", orig.ID, "tier", request.Body.Id)
		return AddTierForEntry500TextResponse("Error creating entry tier"), nil
	}

	var resp Id
	resp.Id = insertedTierForEntry.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddEntryTier")
		return AddTierForEntry500TextResponse("Internal server error"), nil
	}

	return AddTierForEntry201JSONResponse(resp), nil

}

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

func (a ApiImpl) GetTiersForEntry(ctx context.Context, request GetTiersForEntryRequestObject) (GetTiersForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTiersForEntry401TextResponse("Access denied"), nil
	}

	//Get the entry
	var entry db.Entry
	var err error

	if request.Key == GetTiersForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetTiersForEntry400TextResponse("Error parsing id"), nil
		}
		entry, err = a.queries.EntryById(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetTiersForEntry400TextResponse("Unable to parse symbol"), nil
		}
		entry, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTiersForEntry404TextResponse("Entry not found"), nil
		} else {
			slog.ErrorContext(ctx, "Failed to fetch entry", "error", err, "key", request.Key, "value", request.Value)
			return GetTiersForEntry500TextResponse("Internal server error"), nil

		}
	}

	tierList := make([]Tier, 0)

	if entry.Type == string(EntryTypeConsortium) {
		rows, err := a.queries.ListTiersForConsortium(ctx, entry.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to list tiers for consortium", "error", err, "entry", entry.ID)
			return GetTiersForEntry500TextResponse("Internal Server Error"), nil
		}

		for _, row := range rows {
			tier := Tier{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Level:      TierLevel(row.Level),
				Type:       TierType(row.Type),
				Cost:       row.Cost,
			}
			tierList = append(tierList, tier)
		}
	} else {
		rows, err := a.queries.ListTiersForEntry(ctx, entry.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to list tiers for entry", "error", err, "entry", entry.ID)
			return GetTiersForEntry500TextResponse("Internal Server Error"), nil
		}

		for _, row := range rows {
			tier := Tier{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Level:      TierLevel(row.Level),
				Type:       TierType(row.Type),
				Cost:       row.Cost,
			}
			tierList = append(tierList, tier)
		}
	}

	resp := TiersResponse{
		Items: tierList,
		About: About{Count: int64(len(tierList))},
	}

	return GetTiersForEntry200JSONResponse(resp), nil
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

func (a ApiImpl) DeleteTierForEntry(ctx context.Context, request DeleteTierForEntryRequestObject) (DeleteTierForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteTierForEntry401TextResponse("Access denied"), nil
	}

	var entry db.Entry
	var err error
	//Find the entry object
	if request.Key == DeleteTierForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteTierForEntry400TextResponse("Error parsing id"), nil
		}
		entry, err = a.queries.EntryByIdForUpdate(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteTierForEntry400TextResponse("Unable to parse symbol"), nil
		}
		entry, err = a.queries.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404TextResponse("Entry not found"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Entry", "error", err, "key", request.Key, "value", request.Value)
		return DeleteTierForEntry500TextResponse("Internal server error"), nil
	}

	//Find the tier object
	tier, err := a.queries.GetTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404TextResponse("Tier not found"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Tier by Id", "error", err, "tier", request.Id)
		return DeleteTierForEntry500TextResponse("Internal server error"), nil
	}

	//find the entry tier object
	entryTier, err := a.queries.GetEntryTierByTierAndEntry(ctx, db.GetEntryTierByTierAndEntryParams{Tier: tier.ID, Entry: entry.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404TextResponse("Tier not found for entry"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Entry Tier by Tier and Entry", "error", err, "tier", tier.ID, "entry", entry.ID)
		return DeleteTierForEntry500TextResponse("Internal server error"), nil
	}

	err = a.queries.DeleteEntryTierById(ctx, entryTier.ID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete entry tier", "error", err, "id", entryTier.ID)
		return DeleteTierForEntry500TextResponse("Internal server error"), nil
	}
	return DeleteTierForEntry204Response{}, nil

}
