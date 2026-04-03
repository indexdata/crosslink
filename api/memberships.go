package api

import (
	"context"
	"errors"
	"indexdata/directory/auth"
	"indexdata/directory/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddMembership(ctx context.Context, request AddMembershipRequestObject) (AddMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddMembership401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddMembership")
		return AddMembership500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedMembership, err := qtx.CreateMembership(ctx, request.Body.Institution)

	if err != nil {
		slog.ErrorContext(ctx, "failed to create membership", "error", err, "institution", request.Body.Institution)
		return AddMembership500TextResponse("Error creating membership"), nil
	}

	var resp Id
	resp.Id = insertedMembership.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddMembership")
		return AddMembership500TextResponse("Internal server error"), nil
	}

	return AddMembership201JSONResponse(resp), nil

}

func (a ApiImpl) GetMembership(ctx context.Context, request GetMembershipRequestObject) (GetMembershipResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetMembership401TextResponse("Access denied"), nil
	}

	membership, err := a.queries.GetMembershipById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetMembership404TextResponse("Membership not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get membership", "error", err, "id", request.Id)
			return GetMembership500TextResponse("Internal server error"), nil
		}
	}

	membershipResponse := Membership{
		Id:          &membership.ID,
		Institution: membership.Institution,
	}

	return GetMembership200JSONResponse(membershipResponse), nil
}

func (a ApiImpl) GetMemberships(ctx context.Context, request GetMembershipsRequestObject) (GetMembershipsResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetMemberships401TextResponse("Access denied"), nil
	}

	params := db.ListMembershipsParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListMemberships(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list memberships", "error", err)
		return GetMemberships500TextResponse("Internal Server Error"), nil
	}

	membershipList := make([]Membership, 0, len(rows))

	for _, row := range rows {
		membership := Membership{
			Id:          &row.ID,
			Institution: row.Institution,
		}
		membershipList = append(membershipList, membership)
	}

	resp := MembershipsResponse{
		Items: membershipList,
		About: About{
			Count: int64(len(membershipList)),
		},
	}

	return GetMemberships200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteMembership(ctx context.Context, request DeleteMembershipRequestObject) (DeleteMembershipResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteMembership401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteMembershipById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete membership", "error", err, "id", request.Id)
		return DeleteMembership500TextResponse("Internal server error"), nil
	}
	return DeleteMembership204Response{}, nil

}
