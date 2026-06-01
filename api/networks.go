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

func (a ApiImpl) AddNetwork(ctx context.Context, request AddNetworkRequestObject) (AddNetworkResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddNetwork401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddNetwork")
		return AddNetwork500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	insertedNetwork, err := qtx.CreateNetwork(ctx, request.Body.Name)

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
		Id:   &network.ID,
		Name: network.Name,
	}

	return GetNetwork200JSONResponse(networkResponse), nil
}

const defaultOrder = "ORDER BY n.name, n.id"
const defaultLimit = 10

func getCQLQuery(cqlString string, baseArgCount int) (pgcql.Query, error) {
	pgDefinition := pgcql.NewPgDefinition()

	nameField := pgcql.NewFieldString().WithLikeOps()
	nameField.SetColumn("n.name") //We will alias the network object to 'n' in our query
	pgDefinition.AddField("name", nameField)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return pgDefinition.Parse(query, baseArgCount+1)
}

func buildNetworkSQL(whereClause string) string {
	baseQuery := `
	SELECT
		n.id,
		n.name,
		COUNT(*) OVER() as total_count
		FROM networks n	
	`

	if whereClause != "" {
		return baseQuery + "\n" + whereClause
	}

	return baseQuery
}

func scanNetworkRow(rows pgx.Rows) (Network, int, error) {
	var (
		id         uuid.UUID
		name       string
		totalCount int
	)
	if err := rows.Scan(&id, &name, &totalCount); err != nil {
		return Network{}, 0, err
	}

	return Network{
		Id:   &id,
		Name: &name,
	}, totalCount, nil
}

func (a ApiImpl) GetNetworks(ctx context.Context, request GetNetworksRequestObject) (GetNetworksResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	var query string
	var args []interface{}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetNetworks401TextResponse("Access denied"), nil
	}

	if request.Params.Q != nil && *request.Params.Q != "" {
		baseArgCount := 0
		cqlQuery, err := getCQLQuery(*request.Params.Q, baseArgCount)
		if err != nil {
			return GetNetworks400TextResponse(fmt.Sprintf("CQL parse error: %v", err)), nil
		}

		whereClause := cqlQuery.GetWhereClause()
		if whereClause != "" {
			whereClause = "WHERE " + whereClause
		}

		query = buildNetworkSQL(whereClause + "\n" + defaultOrder)
		args = cqlQuery.GetQueryArguments()
	} else {
		query = buildNetworkSQL(defaultOrder)
		args = []interface{}{}
	}

	limit := derefOrDefault(request.Params.Limit, defaultLimit)
	args = append(args, limit)
	query += fmt.Sprintf("\nLIMIT $%d", len(args))

	offset := derefOrDefault(request.Params.Offset, 0)
	if offset != 0 {
		args = append(args, offset)
		query += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list networks", "error", err)
		return GetNetworks500TextResponse("Internal Server Error"), nil
	}
	defer rows.Close()

	networkList := make([]Network, 0)
	var totalCount int

	for rows.Next() {
		network, count, err := scanNetworkRow(rows)
		if err != nil {
			slog.ErrorContext(ctx, "failed to scan network row", "error", err)
			return GetNetworks500TextResponse("Internal server error"), nil
		}
		networkList = append(networkList, network)
		totalCount = count
	}

	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "error iterating network rows", "error", err)
		return GetNetworks500TextResponse("Internal server error"), nil
	}

	resp := NetworksResponse{
		Items: networkList,
		About: About{
			Count: int64(totalCount),
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
