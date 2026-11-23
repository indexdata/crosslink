package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/db"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddTierForEntry(ctx context.Context, request AddTierForEntryRequestObject) (AddTierForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddTierForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body == nil || request.Body.Id == uuid.Nil {
		return AddTierForEntry400JSONResponse{badRequest("You must provide a valid tier to add")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	//Find the Entry

	qtx := a.queries.WithTx(tx)

	var orig db.Entry

	switch request.Key {
	case AddTierForEntryParamsKeyById:
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return AddTierForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	case AddTierForEntryParamsKeyBySymbol:
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return AddTierForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return AddTierForEntry404JSONResponse{notFound("Entry not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch entry: %w", err)
	}

	insertedTierForEntry, err := qtx.CreateEntryTier(ctx, db.CreateEntryTierParams{
		Entry: orig.ID,
		Tier:  request.Body.Id,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to add tier to entry entry=%v tier=%v: %w", orig.ID, request.Body.Id, err)
	}

	var resp Id
	resp.Id = insertedTierForEntry.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddTierForEntry201JSONResponse(resp), nil

}

func (a ApiImpl) AddEntryTier(ctx context.Context, request AddEntryTierRequestObject) (AddEntryTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddEntryTier403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body == nil || request.Body.Entry == uuid.Nil || request.Body.Tier == uuid.Nil {
		return AddEntryTier400JSONResponse{badRequest("You must provide a valid entry and tier")}, nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedEntryTier, err := qtx.CreateEntryTier(ctx, db.CreateEntryTierParams{
		Entry: request.Body.Entry,
		Tier:  request.Body.Tier,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create entry tier entry=%v tier=%v: %w", request.Body.Entry, request.Body.Tier, err)
	}

	var resp Id
	resp.Id = insertedEntryTier.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddEntryTier201JSONResponse(resp), nil

}

func (a ApiImpl) GetEntryTierByID(ctx context.Context, request GetEntryTierByIDRequestObject) (GetEntryTierByIDResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetEntryTierByID403JSONResponse{forbidden("Access denied")}, nil
	}

	entryTier, err := a.queries.GetEntryTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetEntryTierByID404JSONResponse{notFound("Entry tier not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to get entry tier id=%v: %w", request.Id, err)
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
		slog.DebugContext(ctx, "permission denied")
		return GetEntryTiers403JSONResponse{forbidden("Access denied")}, nil
	}

	queryField, queryValue, err := SplitQuery(request.Params.Q)

	if err != nil {
		return GetEntryTiers400JSONResponse{badRequest("Malformed query string")}, nil
	}

	params := db.ListEntryTiersParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
		Field:  queryField,
		Value:  queryValue,
	}

	rows, err := a.queries.ListEntryTiers(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list entry tiers: %w", err)
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
		slog.DebugContext(ctx, "permission denied")
		return GetTiersForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	//Get the entry
	var entry db.Entry
	var err error

	if request.Key == GetTiersForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetTiersForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		entry, err = a.queries.EntryById(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetTiersForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		entry, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTiersForEntry404JSONResponse{notFound("Entry not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to fetch entry key=%v value=%v: %w", request.Key, request.Value, err)

		}
	}

	tierList := make([]Tier, 0)

	if entry.Type == string(EntryTypeConsortium) {
		rows, err := a.queries.ListTiersForConsortium(ctx, entry.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list tiers for consortium entry=%v: %w", entry.ID, err)
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
			return nil, fmt.Errorf("failed to list tiers for entry entry=%v: %w", entry.ID, err)
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
		slog.DebugContext(ctx, "permission denied")
		return DeleteEntryTier403JSONResponse{forbidden("Access denied")}, nil
	}

	err := a.queries.DeleteEntryTierById(ctx, request.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete entry tier id=%v: %w", request.Id, err)
	}
	return DeleteEntryTier204Response{}, nil

}

func (a ApiImpl) DeleteTierForEntry(ctx context.Context, request DeleteTierForEntryRequestObject) (DeleteTierForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return DeleteTierForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	var entry db.Entry
	var err error
	//Find the entry object
	if request.Key == DeleteTierForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteTierForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		entry, err = a.queries.EntryByIdForUpdate(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteTierForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		entry, err = a.queries.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404JSONResponse{notFound("Entry not found")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Entry key=%v value=%v: %w", request.Key, request.Value, err)
	}

	//Find the tier object
	tier, err := a.queries.GetTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404JSONResponse{notFound("Tier not found")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Tier by Id tier=%v: %w", request.Id, err)
	}

	//find the entry tier object
	entryTier, err := a.queries.GetEntryTierByTierAndEntry(ctx, db.GetEntryTierByTierAndEntryParams{Tier: tier.ID, Entry: entry.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteTierForEntry404JSONResponse{notFound("Tier not found for entry")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Entry Tier by Tier and Entry tier=%v entry=%v: %w", tier.ID, entry.ID, err)
	}

	err = a.queries.DeleteEntryTierById(ctx, entryTier.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete entry tier id=%v: %w", entryTier.ID, err)
	}
	return DeleteTierForEntry204Response{}, nil

}
