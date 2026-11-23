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

func (a ApiImpl) AddTier(ctx context.Context, request AddTierRequestObject) (AddTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddTier403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body == nil || request.Body.Consortium == uuid.Nil {
		return AddTier400JSONResponse{badRequest("You must provide a consortium")}, nil
	}
	if strings.TrimSpace(request.Body.Name) == "" {
		return AddTier400JSONResponse{badRequest("You must provide a tier name")}, nil
	}

	consortium, err := a.queries.EntryById(ctx, request.Body.Consortium)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddTier400JSONResponse{badRequest("Consortium not found")}, nil
		}
		return nil, fmt.Errorf("failed to get consortium consortium=%v: %w", request.Body.Consortium, err)
	}

	if consortium.Type != string(EntryTypeConsortium) {
		return AddTier400JSONResponse{badRequest("Entry is not a consortium")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	level := request.Body.Level
	switch level {
	case Express, Normal, Rush, Secondarymail, Standard, Urgent:
	default:
		return AddTier400JSONResponse{badRequest("Invalid tier level")}, nil
	}

	tierType := request.Body.Type
	switch tierType {
	case Loan, Copy:
	default:
		return AddTier400JSONResponse{badRequest("Invalid tier type")}, nil
	}
	insertedTier, err := qtx.CreateTier(ctx, db.CreateTierParams{
		Name:       request.Body.Name,
		Consortium: request.Body.Consortium,
		Level:      string(level),
		Type:       string(tierType),
		Cost:       request.Body.Cost,
	})

	if err != nil {
		if isUniqueConstraintViolation(err, "tiers_consortium_name_unique") {
			return AddTier409JSONResponse{Error: "A tier with this name already exists in the consortium"}, nil
		}
		return nil, fmt.Errorf("failed to create tier name=%v: %w", request.Body.Name, err)
	}

	var resp Id
	resp.Id = insertedTier.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddTier201JSONResponse(resp), nil

}

func (a ApiImpl) GetTier(ctx context.Context, request GetTierRequestObject) (GetTierResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetTier403JSONResponse{forbidden("Access denied")}, nil
	}

	tier, err := a.queries.GetTierById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTier404JSONResponse{notFound("Tier not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to get tier id=%v: %w", request.Id, err)
		}
	}

	tierResponse := Tier{
		Id:         &tier.ID,
		Consortium: tier.Consortium,
		Name:       tier.Name,
		Level:      TierLevel(tier.Level),
		Type:       TierType(tier.Type),
		Cost:       tier.Cost,
	}

	return GetTier200JSONResponse(tierResponse), nil
}

func (a ApiImpl) GetTiers(ctx context.Context, request GetTiersRequestObject) (GetTiersResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.PublicUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetTiers403JSONResponse{forbidden("Access denied")}, nil
	}

	params := db.ListTiersParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListTiers(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list tiers: %w", err)
	}

	tierList := make([]Tier, 0, len(rows))

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
		slog.DebugContext(ctx, "permission denied")
		return DeleteTier403JSONResponse{forbidden("Access denied")}, nil
	}

	err := a.queries.DeleteTierById(ctx, request.Id)
	if isForeignKeyViolation(err, "entry_tiers_tier_fkey") {
		return DeleteTier409JSONResponse{Error: "The tier is still assigned to an entry"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to delete tier id=%v: %w", request.Id, err)
	}
	return DeleteTier204Response{}, nil

}

func (a ApiImpl) UpdateTier(ctx context.Context, request UpdateTierRequestObject) (UpdateTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateTier403JSONResponse{forbidden("Access denied")}, nil
	}
	if request.Body == nil {
		return UpdateTier400JSONResponse{badRequest("You must provide fields to update")}, nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)
	orig, err := qtx.GetTierByIdForUpdate(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateTier404JSONResponse{notFound("Tier not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get tier for update id=%v: %w", request.Id, err)
	}

	level := orig.Level
	if request.Body.Level != nil {
		level = string(*request.Body.Level)
	}
	tierType := orig.Type
	if request.Body.Type != nil {
		tierType = string(*request.Body.Type)
	}
	cost := derefOrDefault(request.Body.Cost, orig.Cost)

	switch TierLevel(level) {
	case Express, Normal, Rush, Secondarymail, Standard, Urgent:
	default:
		return UpdateTier400JSONResponse{badRequest("Invalid tier level")}, nil
	}
	switch TierType(tierType) {
	case Loan, Copy:
	default:
		return UpdateTier400JSONResponse{badRequest("Invalid tier type")}, nil
	}
	err = qtx.UpdateTier(ctx, db.UpdateTierParams{
		ID:    request.Id,
		Level: level,
		Type:  tierType,
		Cost:  cost,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update tier id=%v: %w", request.Id, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return UpdateTier204Response{}, nil
}
