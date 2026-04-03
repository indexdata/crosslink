package api

import (
	"context"
	"errors"
	"indexdata/directory/auth"
	"indexdata/directory/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddTierMembership(ctx context.Context, request AddTierMembershipRequestObject) (AddTierMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddTierMembership401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddTierMembership")
		return AddTierMembership500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedTierMembership, err := qtx.CreateTierMembership(ctx, db.CreateTierMembershipParams{
		Membership: request.Body.Membership,
		Tier:       request.Body.Tier,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create tier membership", "error", err, "membership", request.Body.Membership, "tier", request.Body.Tier)
		return AddTierMembership500TextResponse("Error creating tier membership"), nil
	}

	var resp Id
	resp.Id = insertedTierMembership.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddTierMembership")
		return AddTierMembership500TextResponse("Internal server error"), nil
	}

	return AddTierMembership201JSONResponse(resp), nil

}

func (a ApiImpl) GetTierMembership(ctx context.Context, request GetTierMembershipRequestObject) (GetTierMembershipResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTierMembership401TextResponse("Access denied"), nil
	}

	tierMembership, err := a.queries.GetTierMembershipById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTierMembership404TextResponse("TierMembership not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get tierMembership", "error", err, "id", request.Id)
			return GetTierMembership500TextResponse("Internal server error"), nil
		}
	}

	tierMembershipResponse := TierMembership{
		Id:         &tierMembership.ID,
		Membership: tierMembership.Membership,
		Tier:       tierMembership.Tier,
	}

	return GetTierMembership200JSONResponse(tierMembershipResponse), nil
}

func (a ApiImpl) GetTierMemberships(ctx context.Context, request GetTierMembershipsRequestObject) (GetTierMembershipsResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTierMemberships401TextResponse("Access denied"), nil
	}

	params := db.ListTierMembershipsParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListTierMemberships(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list tierMemberships", "error", err)
		return GetTierMemberships500TextResponse("Internal Server Error"), nil
	}

	tierMembershipList := make([]TierMembership, 0, len(rows))

	for _, row := range rows {
		tierMembership := TierMembership{
			Id:         &row.ID,
			Membership: row.Membership,
			Tier:       row.Tier,
		}
		tierMembershipList = append(tierMembershipList, tierMembership)
	}

	resp := TierMembershipsResponse{
		Items: tierMembershipList,
		About: About{
			Count: int64(len(tierMembershipList)),
		},
	}

	return GetTierMemberships200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteTierMembership(ctx context.Context, request DeleteTierMembershipRequestObject) (DeleteTierMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteTierMembership401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteTierMembershipById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete tierMembership", "error", err, "id", request.Id)
		return DeleteTierMembership500TextResponse("Internal server error"), nil
	}
	return DeleteTierMembership204Response{}, nil

}
