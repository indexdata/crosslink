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

func (a ApiImpl) AddNetwork(ctx context.Context, request AddNetworkRequestObject) (AddNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddNetwork401TextResponse("Access denied"), nil
	}

	if request.Body == nil || request.Body.Consortium == uuid.Nil {
		return AddNetwork400TextResponse("You must provide a consortium"), nil
	}

	consortium, err := a.queries.EntryById(ctx, request.Body.Consortium)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddNetwork400TextResponse("Consortium not found"), nil
		}
		slog.ErrorContext(ctx, "failed to get consortium", "error", err, "consortium", request.Body.Consortium)
		return AddNetwork500TextResponse("Internal server error"), nil
	}

	if consortium.Type != string(EntryTypeConsortium) {
		return AddNetwork400TextResponse("Entry is not a consortium"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddNetwork")
		return AddNetwork500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedNetwork, err := qtx.CreateNetwork(ctx, db.CreateNetworkParams{
		Name:       request.Body.Name,
		Consortium: request.Body.Consortium,
		Priority:   request.Body.Priority,
		Reciprocal: request.Body.Reciprocal,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create network", "error", err, "name", request.Body.Name)
		return AddNetwork500TextResponse("Error creating network"), nil
	}

	var resp Id
	resp.Id = insertedNetwork.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddNetwork")
		return AddNetwork500TextResponse("Internal server error"), nil
	}

	return AddNetwork201JSONResponse(resp), nil

}

func (a ApiImpl) GetNetwork(ctx context.Context, request GetNetworkRequestObject) (GetNetworkResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetwork401TextResponse("Access denied"), nil
	}

	network, err := a.queries.GetNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetNetwork404TextResponse("Network not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get network", "error", err, "id", request.Id)
			return GetNetwork500TextResponse("Internal server error"), nil
		}
	}

	networkResponse := Network{
		Id:         &network.ID,
		Consortium: network.Consortium,
		Name:       network.Name,
		Priority:   network.Priority,
		Reciprocal: network.Reciprocal,
	}

	return GetNetwork200JSONResponse(networkResponse), nil
}

func (a ApiImpl) GetNetworks(ctx context.Context, request GetNetworksRequestObject) (GetNetworksResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetworks401TextResponse("Access denied"), nil
	}

	params := db.ListNetworksParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListNetworks(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list networks", "error", err)
		return GetNetworks500TextResponse("Internal Server Error"), nil
	}

	networkList := make([]Network, 0, len(rows))

	for _, row := range rows {
		network := Network{
			Id:         &row.ID,
			Consortium: row.Consortium,
			Name:       row.Name,
			Priority:   row.Priority,
			Reciprocal: row.Reciprocal,
		}
		networkList = append(networkList, network)
	}

	resp := NetworksResponse{
		Items: networkList,
		About: About{
			Count: int64(len(networkList)),
		},
	}

	return GetNetworks200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteNetwork(ctx context.Context, request DeleteNetworkRequestObject) (DeleteNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteNetwork401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteNetworkById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete network", "error", err, "id", request.Id)
		return DeleteNetwork500TextResponse("Internal server error"), nil
	}
	return DeleteNetwork204Response{}, nil

}
