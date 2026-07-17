package api

import (
	"context"
	"errors"
	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/db"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddEntryNetwork(ctx context.Context, request AddEntryNetworkRequestObject) (AddEntryNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddEntryNetwork401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddEntryNetwork")
		return AddEntryNetwork500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if request.Body == nil || request.Body.Entry == uuid.Nil || request.Body.Network == uuid.Nil {
		return AddEntryNetwork400TextResponse("You most provide a valid entry and network"), nil
	}

	qtx := a.queries.WithTx(tx)

	insertedEntryNetwork, err := qtx.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{
		Entry:   request.Body.Entry,
		Network: request.Body.Network,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create entry network", "error", err, "entry", request.Body.Entry, "network", request.Body.Network)
		return AddEntryNetwork500TextResponse("Error creating entry network"), nil
	}

	var resp Id
	resp.Id = insertedEntryNetwork.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddEntryNetwork")
		return AddEntryNetwork500TextResponse("Internal server error"), nil
	}

	return AddEntryNetwork201JSONResponse(resp), nil

}

func (a ApiImpl) AddNetworkForEntry(ctx context.Context, request AddNetworkForEntryRequestObject) (AddNetworkForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddNetworkForEntry401TextResponse("Access denied"), nil
	}

	if request.Body == nil || request.Body.Id == uuid.Nil {
		return AddNetworkForEntry400TextResponse("You must provide a valid network to add"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddNetworkForEntry")
		return AddNetworkForEntry500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	var orig db.Entry

	if request.Key == AddNetworkForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return AddNetworkForEntry400TextResponse("Error parsing id"), nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	} else if request.Key == AddNetworkForEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return AddNetworkForEntry400TextResponse("Unable to parse symbol"), nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return AddNetworkForEntry404TextResponse("Entry not found"), nil
	} else if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch entry", "error", err)
		return AddNetworkForEntry500TextResponse("Internal server error"), nil
	}

	insertedNetworkForEntry, err := qtx.CreateEntryNetwork(ctx, db.CreateEntryNetworkParams{
		Entry:   orig.ID,
		Network: request.Body.Id,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to add network to entry", "error", err, "entry", orig.ID, "network", request.Body.Id)
		return AddNetworkForEntry500TextResponse("Error creating entry network"), nil
	}

	var resp Id
	resp.Id = insertedNetworkForEntry.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddNetworkForEntry")
		return AddNetworkForEntry500TextResponse("Internal server error"), nil
	}

	return AddNetworkForEntry201JSONResponse(resp), nil

}

func (a ApiImpl) GetEntryNetworkByID(ctx context.Context, request GetEntryNetworkByIDRequestObject) (GetEntryNetworkByIDResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntryNetworkByID401TextResponse("Access denied"), nil
	}

	entryNetwork, err := a.queries.GetEntryNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetEntryNetworkByID404TextResponse("Entry Network not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get entry network", "error", err, "id", request.Id)
			return GetEntryNetworkByID500TextResponse("Internal server error"), nil
		}
	}

	entryNetworkResponse := EntryNetwork{
		Id:      &entryNetwork.ID,
		Entry:   entryNetwork.Entry,
		Network: entryNetwork.Network,
	}

	return GetEntryNetworkByID200JSONResponse(entryNetworkResponse), nil
}

func (a ApiImpl) GetNetworksForEntry(ctx context.Context, request GetNetworksForEntryRequestObject) (GetNetworksForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetworksForEntry401TextResponse("Access denied"), nil
	}

	var entry db.Entry
	var err error

	if request.Key == GetNetworksForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetNetworksForEntry400TextResponse("Error parsing id"), nil
		}
		entry, err = a.queries.EntryById(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetNetworksForEntry400TextResponse("Unable to parse symbol"), nil
		}
		entry, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetNetworksForEntry404TextResponse("Entry not found"), nil
		} else {
			slog.ErrorContext(ctx, "Failed to fetch entry", "error", err, "key", request.Key, "value", request.Value)
			return GetNetworksForEntry500TextResponse("Internal server error"), nil
		}
	}

	networkList := make([]Network, 0)

	if entry.Type == string(EntryTypeConsortium) {
		rows, err := a.queries.ListNetworksForConsortium(ctx, entry.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to list networks for consortium", "error", err, "entry", entry.ID)
			return GetNetworksForEntry500TextResponse("Internal Server Error"), nil
		}

		for _, row := range rows {
			network := Network{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Priority:   row.Priority,
			}
			networkList = append(networkList, network)
		}
	} else {
		rows, err := a.queries.ListNetworksForEntry(ctx, entry.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to list networks for entry", "error", err, "entry", entry.ID)
			return GetNetworksForEntry500TextResponse("Internal Server Error"), nil
		}

		for _, row := range rows {
			network := Network{
				Id:         &row.ID,
				Consortium: row.Consortium,
				Name:       row.Name,
				Priority:   row.Priority,
			}
			networkList = append(networkList, network)
		}
	}

	resp := NetworksResponse{
		Items: networkList,
		About: About{Count: int64(len(networkList))},
	}

	return GetNetworksForEntry200JSONResponse(resp), nil
}

func (a ApiImpl) GetEntryNetworks(ctx context.Context, request GetEntryNetworksRequestObject) (GetEntryNetworksResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntryNetworks401TextResponse("Access denied"), nil
	}

	queryField, queryValue, err := SplitQuery(request.Params.Q)

	if err != nil {
		return GetEntryNetworks400TextResponse("Malformed query string"), nil
	}

	params := db.ListEntryNetworksParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
		Field:  queryField,
		Value:  queryValue,
	}

	rows, err := a.queries.ListEntryNetworks(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list entry networks", "error", err)
		return GetEntryNetworks500TextResponse("Internal Server Error"), nil
	}

	entryNetworkList := make([]EntryNetwork, 0, len(rows))

	for _, row := range rows {
		entryNetwork := EntryNetwork{
			Id:      &row.ID,
			Entry:   row.Entry,
			Network: row.Network,
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

func (a ApiImpl) DeleteNetworkForEntry(ctx context.Context, request DeleteNetworkForEntryRequestObject) (DeleteNetworkForEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteNetworkForEntry401TextResponse("Access denied"), nil
	}

	var entry db.Entry
	var err error
	if request.Key == DeleteNetworkForEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteNetworkForEntry400TextResponse("Error parsing id"), nil
		}
		entry, err = a.queries.EntryByIdForUpdate(ctx, parsedId)
	} else {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteNetworkForEntry400TextResponse("Unable to parse symbol"), nil
		}
		entry, err = a.queries.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404TextResponse("Entry not found"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Entry", "error", err, "key", request.Key, "value", request.Value)
		return DeleteNetworkForEntry500TextResponse("Internal server error"), nil
	}

	network, err := a.queries.GetNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404TextResponse("Network not found"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Network by Id", "error", err, "network", request.Id)
		return DeleteNetworkForEntry500TextResponse("Internal server error"), nil
	}

	entryNetwork, err := a.queries.GetEntryNetworkByNetworkAndEntry(ctx, db.GetEntryNetworkByNetworkAndEntryParams{Network: network.ID, Entry: entry.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteNetworkForEntry404TextResponse("Network not found for entry"), nil
		}
		slog.ErrorContext(ctx, "Failed to retrieve Entry Network by Network and Entry", "error", err, "network", network.ID, "entry", entry.ID)
		return DeleteNetworkForEntry500TextResponse("Internal server error"), nil
	}

	err = a.queries.DeleteEntryNetworkById(ctx, entryNetwork.ID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete entry network", "error", err, "id", entryNetwork.ID)
		return DeleteNetworkForEntry500TextResponse("Internal server error"), nil
	}
	return DeleteNetworkForEntry204Response{}, nil

}

func (a ApiImpl) DeleteEntryNetwork(ctx context.Context, request DeleteEntryNetworkRequestObject) (DeleteEntryNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteEntryNetwork401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteEntryNetworkById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete entry network", "error", err, "id", request.Id)
		return DeleteEntryNetwork500TextResponse("Internal server error"), nil
	}
	return DeleteEntryNetwork204Response{}, nil

}
