package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddNetwork(ctx context.Context, request AddNetworkRequestObject) (AddNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddNetwork403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body == nil || request.Body.Consortium == uuid.Nil {
		return AddNetwork400JSONResponse{badRequest("You must provide a consortium")}, nil
	}
	if strings.TrimSpace(request.Body.Name) == "" {
		return AddNetwork400JSONResponse{badRequest("You must provide a network name")}, nil
	}

	consortium, err := a.queries.EntryById(ctx, request.Body.Consortium)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddNetwork400JSONResponse{badRequest("Consortium not found")}, nil
		}
		return nil, fmt.Errorf("failed to get consortium consortium=%v: %w", request.Body.Consortium, err)
	}

	if consortium.Type != string(EntryTypeConsortium) {
		return AddNetwork400JSONResponse{badRequest("Entry is not a consortium")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedNetwork, err := qtx.CreateNetwork(ctx, db.CreateNetworkParams{
		Name:       request.Body.Name,
		Consortium: request.Body.Consortium,
		Reciprocal: request.Body.Reciprocal,
	})

	if err != nil {
		if isUniqueConstraintViolation(err, "networks_consortium_name_unique") {
			return AddNetwork409JSONResponse{Error: "A network with this name already exists in the consortium"}, nil
		}
		return nil, fmt.Errorf("failed to create network name=%v: %w", request.Body.Name, err)
	}

	var resp Id
	resp.Id = insertedNetwork.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddNetwork201JSONResponse(resp), nil

}

func (a ApiImpl) GetNetwork(ctx context.Context, request GetNetworkRequestObject) (GetNetworkResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetNetwork403JSONResponse{forbidden("Access denied")}, nil
	}

	network, err := a.queries.GetNetworkById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetNetwork404JSONResponse{notFound("Network not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to get network id=%v: %w", request.Id, err)
		}
	}

	networkResponse := Network{
		Id:         &network.ID,
		Consortium: network.Consortium,
		Name:       network.Name,
		Reciprocal: network.Reciprocal,
	}

	return GetNetwork200JSONResponse(networkResponse), nil
}

func (a ApiImpl) GetNetworks(ctx context.Context, request GetNetworksRequestObject) (GetNetworksResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetNetworks403JSONResponse{forbidden("Access denied")}, nil
	}

	params := db.ListNetworksParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListNetworks(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	networkList := make([]Network, 0, len(rows))

	for _, row := range rows {
		network := Network{
			Id:         &row.ID,
			Consortium: row.Consortium,
			Name:       row.Name,
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
		slog.DebugContext(ctx, "permission denied")
		return DeleteNetwork403JSONResponse{forbidden("Access denied")}, nil
	}

	err := a.queries.DeleteNetworkById(ctx, request.Id)
	if isForeignKeyViolation(err, "entry_networks_network_fkey") {
		return DeleteNetwork409JSONResponse{Error: "The network is still assigned to an entry"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to delete network id=%v: %w", request.Id, err)
	}
	return DeleteNetwork204Response{}, nil

}

func (a ApiImpl) UpdateNetwork(ctx context.Context, request UpdateNetworkRequestObject) (UpdateNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateNetwork403JSONResponse{forbidden("Access denied")}, nil
	}
	if request.Body == nil {
		return UpdateNetwork400JSONResponse{badRequest("You must provide fields to update")}, nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)
	orig, err := qtx.GetNetworkByIdForUpdate(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateNetwork404JSONResponse{notFound("Network not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get network for update id=%v: %w", request.Id, err)
	}

	reciprocal := orig.Reciprocal
	if request.Body.Reciprocal.IsSpecified() {
		if request.Body.Reciprocal.IsNull() {
			reciprocal = nil
		} else {
			value := request.Body.Reciprocal.MustGet()
			reciprocal = &value
		}
	}

	err = qtx.UpdateNetwork(ctx, db.UpdateNetworkParams{
		ID:         request.Id,
		Reciprocal: reciprocal,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update network id=%v: %w", request.Id, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return UpdateNetwork204Response{}, nil
}
