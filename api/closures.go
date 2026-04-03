package api

import (
	"context"
	"errors"
	"indexdata/directory/auth"
	"indexdata/directory/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddClosure(ctx context.Context, request AddClosureRequestObject) (AddClosureResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return AddClosure401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddClosure")
		return AddClosure500TextResponse("Internal server error"), nil
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
		slog.ErrorContext(ctx, "failed to create closure", "error", err)
		return AddClosure500TextResponse("Error creating closure"), nil
	}

	var resp Id
	resp.Id = insertedClosure.ID

	err = tx.Commit(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddClosure")
		return AddClosure500TextResponse("Internal server error"), nil
	}

	return AddClosure201JSONResponse(resp), nil

}

func (a ApiImpl) GetClosure(ctx context.Context, request GetClosureRequestObject) (GetClosureResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetClosure401TextResponse("Access denied"), nil
	}

	closure, err := a.queries.GetClosureById(ctx, request.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetClosure404TextResponse("Closure not found"), nil
		} else {
			slog.ErrorContext(ctx, "failed to get closure", "error", err, "id", request.Id)
			return GetClosure500TextResponse("Internal server error"), nil
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
		slog.ErrorContext(ctx, "permission denied")
		return GetClosures401TextResponse("Access denied"), nil
	}

	params := db.ListClosuresParams{
		Limit:  derefOrDefault(request.Params.Limit, 1000),
		Offset: derefOrDefault(request.Params.Offset, 0),
	}

	rows, err := a.queries.ListClosures(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list closures", "error", err)
		return GetClosures500TextResponse("Internal Server Error"), nil
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
		slog.ErrorContext(ctx, "permission denied")
		return DeleteClosure401TextResponse("Access denied"), nil
	}

	err := a.queries.DeleteClosureById(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete Closure", "error", err, "id", request.Id)
		return DeleteClosure500TextResponse("Internal server error"), nil
	}
	return DeleteClosure204Response{}, nil

}

func (a ApiImpl) UpdateClosure(ctx context.Context, request UpdateClosureRequestObject) (UpdateClosureResponseObject, error) {
	authData := auth.GetAuthData(ctx)

	if !authData.HasRole(auth.ConsortialAdminRole) {
		slog.ErrorContext(ctx, "permission denied")
		return UpdateClosure401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)

	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "UpdateClosure")
		return UpdateClosure500TextResponse("Internal server error"), nil
	}

	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	orig, err := qtx.GetClosureByIdForUpdate(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateClosure404TextResponse("Closure not found"), nil
	} else if err != nil {
		slog.ErrorContext(ctx, "failed to get closure for update", "error", err, "id", request.Id)
		return UpdateClosure500TextResponse("Internal server error"), nil
	}

	err = qtx.UpdateClosure(ctx, db.UpdateClosureParams{
		ID:        request.Id,
		StartDate: updateIfValidDate(request.Body.StartDate, orig.StartDate),
		EndDate:   updateIfValidDate(request.Body.EndDate, orig.EndDate),
		Reason:    derefOrDefault(request.Body.Reason, orig.Reason),
	})

	if err != nil {
		slog.ErrorContext(ctx, "failed to update closure", "error", err, "id", request.Id)
		return UpdateClosure500TextResponse("internal server error"), nil
	}

	err = tx.Commit(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "UpdateClosure")
		return UpdateClosure500TextResponse("Internal server error"), nil
	}

	return UpdateClosure204Response{}, nil
}
