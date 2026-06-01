package api

import (
	"context"
	"errors"
	"fmt"
	"indexdata/directory/auth"
	"log/slog"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddTier(ctx context.Context, request AddTierRequestObject) (AddTierResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddTier401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddTier")
		return AddTier500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedTier, err := qtx.CreateTier(ctx, request.Body.Name)

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
		Id:   &tier.ID,
		Name: tier.Name,
	}

	return GetTier200JSONResponse(tierResponse), nil
}

const defaultTierOrder = "ORDER BY t.name, t.id"
const defaultTierLimit = 1000

func getTierCQLQuery(cqlString string, baseArgCount int) (pgcql.Query, error) {
	pgDefinition := pgcql.NewPgDefinition()

	nameField := pgcql.NewFieldString().WithLikeOps()
	nameField.SetColumn("t.name")
	pgDefinition.AddField("name", nameField)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return pgDefinition.Parse(query, baseArgCount+1)
}

func buildTierSQL(whereClause string) string {
	baseQuery := `
	SELECT
		t.id,
		t.name,
		COUNT(*) OVER() as total_count
		FROM tiers t
	`

	if whereClause != "" {
		return baseQuery + "\n" + whereClause
	}

	return baseQuery
}

func scanTierRow(rows pgx.Rows) (Tier, int, error) {
	var (
		id         uuid.UUID
		name       string
		totalCount int
	)
	if err := rows.Scan(&id, &name, &totalCount); err != nil {
		return Tier{}, 0, err
	}

	return Tier{
		Id:   &id,
		Name: &name,
	}, totalCount, nil
}

func (a ApiImpl) GetTiers(ctx context.Context, request GetTiersRequestObject) (GetTiersResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.PublicUserRole}

	var query string
	var args []interface{}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetTiers401TextResponse("Access denied"), nil
	}

	if request.Params.Q != nil && *request.Params.Q != "" {
		baseArgCount := 0
		cqlQuery, err := getTierCQLQuery(*request.Params.Q, baseArgCount)
		if err != nil {
			return GetTiers400TextResponse(fmt.Sprintf("CQL parse error: %v", err)), nil
		}

		whereClause := cqlQuery.GetWhereClause()
		if whereClause != "" {
			whereClause = "WHERE " + whereClause
		}

		query = buildTierSQL(whereClause + "\n" + defaultTierOrder)
		args = cqlQuery.GetQueryArguments()
	} else {
		query = buildTierSQL(defaultTierOrder)
		args = []interface{}{}
	}

	limit := derefOrDefault(request.Params.Limit, defaultTierLimit)
	args = append(args, limit)
	query += fmt.Sprintf("\nLIMIT $%d", len(args))

	offset := derefOrDefault(request.Params.Offset, 0)
	if offset != 0 {
		args = append(args, offset)
		query += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list tiers", "error", err)
		return GetTiers500TextResponse("Internal Server Error"), nil
	}
	defer rows.Close()

	tierList := make([]Tier, 0)
	var totalCount int

	for rows.Next() {
		tier, count, err := scanTierRow(rows)
		if err != nil {
			slog.ErrorContext(ctx, "failed to scan tier row", "error", err)
			return GetTiers500TextResponse("Internal server error"), nil
		}
		tierList = append(tierList, tier)
		totalCount = count
	}

	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "error iterating tier rows", "error", err)
		return GetTiers500TextResponse("Internal server error"), nil
	}

	resp := TiersResponse{
		Items: tierList,
		About: About{
			Count: int64(totalCount),
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
