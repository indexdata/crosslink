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

func (a ApiImpl) AddTier(ctx context.Context, request AddTierRequestObject) (AddTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddTier401TextResponse("Access denied"), nil
	}

	if request.Body == nil || request.Body.Consortium == uuid.Nil {
		return AddTier400TextResponse("You must provide a consortium"), nil
	}

	consortium, err := a.queries.EntryById(ctx, request.Body.Consortium)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddTier400TextResponse("Consortium not found"), nil
		}
		slog.ErrorContext(ctx, "failed to get consortium", "error", err, "consortium", request.Body.Consortium)
		return AddTier500TextResponse("Internal server error"), nil
	}

	if consortium.Type != string(EntryTypeConsortium) {
		return AddTier400TextResponse("Entry is not a consortium"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddTier")
		return AddTier500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedTier, err := qtx.CreateTier(ctx, db.CreateTierParams{
		Name:       request.Body.Name,
		Consortium: request.Body.Consortium,
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to create tier", "error", err, "name", request.Body.Name)
		return AddTier500TextResponse("Error creating tier"), nil
	}

	var resp Id
	resp.Id = insertedTier.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddTier")
		return AddTier500TextResponse("Internal server error"), nil
	}

	return AddTier201JSONResponse(resp), nil

}

func (a ApiImpl) GetTier(ctx context.Context, request GetTierRequestObject) (GetTierResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTier401TextResponse("Access denied"), nil
	}

	tier, err := a.queries.GetTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTier404TextResponse("Tier not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get tier", "error", err, "id", request.Id)
			return GetTier500TextResponse("Internal server error"), nil
		}
	}

	tierResponse := Tier{
		Id:         &tier.ID,
		Consortium: tier.Consortium,
		Name:       tier.Name,
	}

	return GetTier200JSONResponse(tierResponse), nil
}

func (a ApiImpl) GetTiers(ctx context.Context, request GetTiersRequestObject) (GetTiersResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.PublicUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTiers401TextResponse("Access denied"), nil
	}

	params := db.ListTiersParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListTiers(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list tiers", "error", err)
		return GetTiers500TextResponse("Internal Server Error"), nil
	}

	tierList := make([]Tier, 0, len(rows))

	for _, row := range rows {
		tier := Tier{
			Id:         &row.ID,
			Consortium: row.Consortium,
			Name:       row.Name,
		}
		tierList = append(tierList, tier)
	}

	resp := TiersResponse{
		Items: tierList,
		About: About{
			Count: int64(len(tierList)),
		},
	}

	return GetTiers200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteTier(ctx context.Context, request DeleteTierRequestObject) (DeleteTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteTier401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteTierById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete tier", "error", err, "id", request.Id)
		return DeleteTier500TextResponse("Internal server error"), nil
	}
	return DeleteTier204Response{}, nil

}
