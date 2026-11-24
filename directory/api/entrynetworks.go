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

func (a ApiImpl) AddEntryNetwork(ctx context.Context, request AddEntryNetworkRequestObject) (AddEntryNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddEntryNetwork403JSONResponse{forbidden("Access denied")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if request.Body == nil || request.Body.Entry == uuid.Nil || request.Body.Network == uuid.Nil {
		return AddEntryNetwork400JSONResponse{badRequest("You must provide a valid entry and network")}, nil
	}

	qtx := a.queries.WithTx(tx)

	insertedEntryNetwork, err := qtx.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{
		Entry:    request.Body.Entry,
		Network:  request.Body.Network,
		Priority: derefOrDefault(request.Body.Priority, int32(0)),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create entry network entry=%v network=%v: %w", request.Body.Entry, request.Body.Network, err)
	}

	var resp Id
	resp.Id = insertedEntryNetwork.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddEntryNetwork201JSONResponse(resp), nil

}

func (a ApiImpl) AddNetworkForEntry(ctx context.Context, request AddNetworkForEntryRequestObject) (AddNetworkForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddNetworkForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body == nil || request.Body.Id == uuid.Nil {
		return AddNetworkForEntry400JSONResponse{badRequest("You must provide a valid network to add")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	var orig db.Entry

	switch request.Key {
	case AddNetworkForEntryParamsKeyById:
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return AddNetworkForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	case AddNetworkForEntryParamsKeyBySymbol:
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return AddNetworkForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return AddNetworkForEntry404JSONResponse{notFound("Entry not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch entry: %w", err)
	}

	insertedNetworkForEntry, err := qtx.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{
		Entry:    orig.ID,
		Network:  request.Body.Id,
		Priority: derefOrDefault(request.Body.Priority, int32(0)),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to add network to entry entry=%v network=%v: %w", orig.ID, request.Body.Id, err)
	}

	var resp Id
	resp.Id = insertedNetworkForEntry.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddNetworkForEntry201JSONResponse(resp), nil

}

func (a ApiImpl) GetEntryNetworkByID(ctx context.Context, request GetEntryNetworkByIDRequestObject) (GetEntryNetworkByIDResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetEntryNetworkByID403JSONResponse{forbidden("Access denied")}, nil
	}

	entryNetwork, err := a.queries.GetEntryNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetEntryNetworkByID404JSONResponse{notFound("Entry network not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to get entry network id=%v: %w", request.Id, err)
		}
	}

	entryNetworkResponse := EntryNetwork{
		Id:       &entryNetwork.ID,
		Entry:    entryNetwork.Entry,
		Network:  entryNetwork.Network,
		Priority: entryNetwork.Priority,
	}

	return GetEntryNetworkByID200JSONResponse(entryNetworkResponse), nil
}

func (a ApiImpl) GetNetworksForEntry(ctx context.Context, request GetNetworksForEntryRequestObject) (GetNetworksForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetNetworksForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	var entry db.Entry
	var err error

	if request.Key == GetNetworksForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetNetworksForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		entry, err = a.queries.EntryById(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetNetworksForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		entry, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetNetworksForEntry404JSONResponse{notFound("Entry not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to fetch entry key=%v value=%v: %w", request.Key, request.Value, err)
		}
	}

	networkList := make([]NetworkForEntryDetails, 0)

	if entry.Type == string(EntryTypeConsortium) {
		rows, err := a.queries.ListNetworksForConsortium(ctx, entry.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list networks for consortium entry=%v: %w", entry.ID, err)
		}

		for _, row := range rows {
			network := NetworkForEntryDetails{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Reciprocal: row.Reciprocal,
			}
			networkList = append(networkList, network)
		}
	} else {
		rows, err := a.queries.ListNetworksForEntry(ctx, entry.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list networks for entry entry=%v: %w", entry.ID, err)
		}

		for _, row := range rows {
			network := NetworkForEntryDetails{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Priority:   &row.Priority,
				Reciprocal: row.Reciprocal,
			}
			networkList = append(networkList, network)
		}
	}

	resp := NetworksForEntryResponse{
		Items: networkList,
		About: About{Count: int64(len(networkList))},
	}

	return GetNetworksForEntry200JSONResponse(resp), nil
}

func (a ApiImpl) GetEntryNetworks(ctx context.Context, request GetEntryNetworksRequestObject) (GetEntryNetworksResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetEntryNetworks403JSONResponse{forbidden("Access denied")}, nil
	}

	queryField, queryValue, err := SplitQuery(request.Params.Q)

	if err != nil {
		return GetEntryNetworks400JSONResponse{badRequest("Malformed query string")}, nil
	}

	params := db.ListEntryNetworksParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
		Field:  queryField,
		Value:  queryValue,
	}

	rows, err := a.queries.ListEntryNetworks(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list entry networks: %w", err)
	}

	entryNetworkList := make([]EntryNetwork, 0, len(rows))

	for _, row := range rows {
		entryNetwork := EntryNetwork{
			Id:       &row.ID,
			Entry:    row.Entry,
			Network:  row.Network,
			Priority: row.Priority,
		}
		entryNetworkList = append(entryNetworkList, entryNetwork)
	}

	resp := EntryNetworksResponse{
		Items: entryNetworkList,
		About: About{
			Count: int64(len(entryNetworkList)),
		},
	}

	return GetEntryNetworks200JSONResponse(resp), nil

}

func (a ApiImpl) UpdateEntryNetwork(ctx context.Context, request UpdateEntryNetworkRequestObject) (UpdateEntryNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateEntryNetwork403JSONResponse{forbidden("Access denied")}, nil
	}
	if request.Body == nil {
		return UpdateEntryNetwork400JSONResponse{badRequest("You must provide a priority to update")}, nil
	}

	rows, err := a.queries.UpdateEntryNetwork(ctx, db.UpdateEntryNetworkParams{
		ID:       request.Id,
		Priority: request.Body.Priority,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update entry network id=%v: %w", request.Id, err)
	}
	if rows == 0 {
		return UpdateEntryNetwork404JSONResponse{notFound("Entry network not found")}, nil
	}
	return UpdateEntryNetwork204Response{}, nil
}

func (a ApiImpl) DeleteNetworkForEntry(ctx context.Context, request DeleteNetworkForEntryRequestObject) (DeleteNetworkForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return DeleteNetworkForEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	var entry db.Entry
	var err error
	if request.Key == DeleteNetworkForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteNetworkForEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		entry, err = a.queries.EntryByIdForUpdate(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteNetworkForEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		entry, err = a.queries.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404JSONResponse{notFound("Entry not found")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Entry key=%v value=%v: %w", request.Key, request.Value, err)
	}

	network, err := a.queries.GetNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404JSONResponse{notFound("Network not found")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Network by Id network=%v: %w", request.Id, err)
	}

	entryNetwork, err := a.queries.GetEntryNetworkByNetworkAndEntry(ctx, db.GetEntryNetworkByNetworkAndEntryParams{Network: network.ID, Entry: entry.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404JSONResponse{notFound("Network not found for entry")}, nil
		}
		return nil, fmt.Errorf("failed to retrieve Entry Network by Network and Entry network=%v entry=%v: %w", network.ID, entry.ID, err)
	}

	err = a.queries.DeleteEntryNetworkById(ctx, entryNetwork.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete entry network id=%v: %w", entryNetwork.ID, err)
	}
	return DeleteNetworkForEntry204Response{}, nil

}

func (a ApiImpl) DeleteEntryNetwork(ctx context.Context, request DeleteEntryNetworkRequestObject) (DeleteEntryNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return DeleteEntryNetwork403JSONResponse{forbidden("Access denied")}, nil
	}

	err := a.queries.DeleteEntryNetworkById(ctx, request.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete entry network id=%v: %w", request.Id, err)
	}
	return DeleteEntryNetwork204Response{}, nil

}
