package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddClosure(ctx context.Context, request AddClosureRequestObject) (AddClosureResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return AddClosure403JSONResponse{forbidden("Access denied")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	qtx := a.queries.WithTx(tx)

	closureParams := db.CreateClosureParams{
		Entry:     request.Body.Entry,
		StartDate: DatePtrToPgTimestamp(&request.Body.StartDate),
		EndDate:   DatePtrToPgTimestamp(&request.Body.EndDate),
		Reason:    request.Body.Reason,
	}

	insertedClosure, err := qtx.CreateClosure(ctx, closureParams)

	if err != nil {
		return nil, fmt.Errorf("failed to create closure: %w", err)
	}

	var resp Id
	resp.Id = insertedClosure.ID

	err = tx.Commit(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddClosure201JSONResponse(resp), nil

}

func (a ApiImpl) GetClosure(ctx context.Context, request GetClosureRequestObject) (GetClosureResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetClosure403JSONResponse{forbidden("Access denied")}, nil
	}

	closure, err := a.queries.GetClosureById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetClosure404JSONResponse{notFound("Closure not found")}, nil
		} else {
			return nil, fmt.Errorf("failed to get closure id=%v: %w", request.Id, err)
		}
	}

	closureResponse := Closure{
		Id:        &closure.ID,
		StartDate: *PgTimestampToDatePtr(closure.StartDate),
		EndDate:   *PgTimestampToDatePtr(closure.EndDate),
		Reason:    closure.Reason,
		Entry:     closure.Entry,
	}

	return GetClosure200JSONResponse(closureResponse), nil
}

func (a ApiImpl) GetClosures(ctx context.Context, request GetClosuresRequestObject) (GetClosuresResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetClosures403JSONResponse{forbidden("Access denied")}, nil
	}

	params := db.ListClosuresParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListClosures(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list closures: %w", err)
	}

	closures := make([]Closure, 0, len(rows))

	for _, row := range rows {
		closure := Closure{
			Id:        &row.ID,
			StartDate: *PgTimestampToDatePtr(row.StartDate),
			EndDate:   *PgTimestampToDatePtr(row.EndDate),
			Reason:    row.Reason,
			Entry:     row.Entry,
		}
		closures = append(closures, closure)
	}

	resp := ClosuresResponse{
		Items: closures,
		About: About{
			Count: int64(len(closures)),
		},
	}

	return GetClosures200JSONResponse(resp), nil

}

func (a ApiImpl) DeleteClosure(ctx context.Context, request DeleteClosureRequestObject) (DeleteClosureResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return DeleteClosure403JSONResponse{forbidden("Access denied")}, nil
	}

	err := a.queries.DeleteClosureById(ctx, request.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete Closure id=%v: %w", request.Id, err)
	}
	return DeleteClosure204Response{}, nil

}

func (a ApiImpl) UpdateClosure(ctx context.Context, request UpdateClosureRequestObject) (UpdateClosureResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateClosure403JSONResponse{forbidden("Access denied")}, nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	orig, err := qtx.GetClosureByIdForUpdate(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateClosure404JSONResponse{notFound("Closure not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get closure for update id=%v: %w", request.Id, err)
	}

	err = qtx.UpdateClosure(ctx, db.UpdateClosureParams{
		ID:        request.Id,
		StartDate: updateIfValidDate(request.Body.StartDate, orig.StartDate),
		EndDate:   updateIfValidDate(request.Body.EndDate, orig.EndDate),
		Reason:    derefOrDefault(request.Body.Reason, orig.Reason),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update closure id=%v: %w", request.Id, err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return UpdateClosure204Response{}, nil
}
