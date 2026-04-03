package api

import (
	"context"
	"errors"
	"indexdata/directory/auth"
	"indexdata/directory/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddNetworkMembership(ctx context.Context, request AddNetworkMembershipRequestObject) (AddNetworkMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddNetworkMembership401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddNetworkMembership")
		return AddNetworkMembership500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedNetworkMembership, err := qtx.CreateNetworkMembership(ctx, db.CreateNetworkMembershipParams{
		Membership: request.Body.Membership,
		Network:    request.Body.Network,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create network membership", "error", err, "membership", request.Body.Membership, "network", request.Body.Network)
		return AddNetworkMembership500TextResponse("Error creating network membership"), nil
	}

	var resp Id
	resp.Id = insertedNetworkMembership.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddNetworkMembership")
		return AddNetworkMembership500TextResponse("Internal server error"), nil
	}

	return AddNetworkMembership201JSONResponse(resp), nil

}

func (a ApiImpl) GetNetworkMembership(ctx context.Context, request GetNetworkMembershipRequestObject) (GetNetworkMembershipResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetworkMembership401TextResponse("Access denied"), nil
	}

	networkMembership, err := a.queries.GetNetworkMembershipById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetNetworkMembership404TextResponse("NetworkMembership not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get networkMembership", "error", err, "id", request.Id)
			return GetNetworkMembership500TextResponse("Internal server error"), nil
		}
	}

	networkMembershipResponse := NetworkMembership{
		Id:         &networkMembership.ID,
		Membership: networkMembership.Membership,
		Network:    networkMembership.Network,
	}

	return GetNetworkMembership200JSONResponse(networkMembershipResponse), nil
}

func (a ApiImpl) GetNetworkMemberships(ctx context.Context, request GetNetworkMembershipsRequestObject) (GetNetworkMembershipsResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetworkMemberships401TextResponse("Access denied"), nil
	}

	params := db.ListNetworkMembershipsParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListNetworkMemberships(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list networkMemberships", "error", err)
		return GetNetworkMemberships500TextResponse("Internal Server Error"), nil
	}

	networkMembershipList := make([]NetworkMembership, 0, len(rows))

	for _, row := range rows {
		networkMembership := NetworkMembership{
			Id:         &row.ID,
			Membership: row.Membership,
			Network:    row.Network,
		}
		networkMembershipList = append(networkMembershipList, networkMembership)
	}

	resp := NetworkMembershipsResponse{
		Items: networkMembershipList,
		About: About{
			Count: int64(len(networkMembershipList)),
		},
	}

	return GetNetworkMemberships200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteNetworkMembership(ctx context.Context, request DeleteNetworkMembershipRequestObject) (DeleteNetworkMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteNetworkMembership401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteNetworkMembershipById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete networkMembership", "error", err, "id", request.Id)
		return DeleteNetworkMembership500TextResponse("Internal server error"), nil
	}
	return DeleteNetworkMembership204Response{}, nil

}
