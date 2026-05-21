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
