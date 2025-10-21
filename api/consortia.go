package api

import (
	"context"
	"errors"
	"indexdata/directoryish/db"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (a ApiImpl) AddConsortium(ctx context.Context, request AddConsortiumRequestObject) (AddConsortiumResponseObject, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "AddConsortium")
		return AddConsortium500TextResponse("Internal server error"), nil
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	insertedConsortium, err := qtx.CreateConsortium(ctx, db.CreateConsortiumParams{
		Name:  request.Body.Name,
		Entry: request.Body.Entry,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to create consortium", "error", err, "name", request.Body.Name)
		return AddConsortium500TextResponse("Error creating consortium"), nil
	}

	var resp Id
	resp.Id = insertedConsortium.ID

	err = tx.Commit(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "AddConsortium")
		return AddConsortium500TextResponse("Internal server error"), nil
	}

	return AddConsortium201JSONResponse(resp), nil
}

func (a ApiImpl) GetConsortia(ctx context.Context, request GetConsortiaRequestObject) (GetConsortiaResponseObject, error) {
	rows, err := a.queries.ListConsortia(ctx, nil)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list consortia", "error", err)
		return GetConsortia500TextResponse("Internal server error"), nil
	}

	resp := make([]Consortium, 0, len(rows))

	for _, row := range rows {
		resp = append(resp, Consortium{
			Id:    &row.ID,
			Entry: row.Entry,
			Name:  row.Name,
		})
	}

	return GetConsortia200JSONResponse(resp), nil
}

func (a ApiImpl) GetConsortium(ctx context.Context, request GetConsortiumRequestObject) (GetConsortiumResponseObject, error) {
	rows, err := a.queries.ListConsortia(ctx, &request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get consortium", "error", err, "id", request.Id)
		return GetConsortium500TextResponse("Internal server error"), nil
	}

	if len(rows) == 0 {
		return GetConsortium404TextResponse("Consortium not found"), nil
	}

	return GetConsortium200JSONResponse(Consortium{
		Id:    &rows[0].ID,
		Entry: rows[0].Entry,
		Name:  rows[0].Name,
	}), nil
}

func (a ApiImpl) UpdateConsortium(ctx context.Context, request UpdateConsortiumRequestObject) (UpdateConsortiumResponseObject, error) {
	var orig db.Consortium
	orig, err := a.queries.ConsortiumById(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateConsortium404TextResponse("Consortium not found"), nil
	} else if err != nil {
		slog.ErrorContext(ctx, "failed to get consortium for update", "error", err, "id", request.Id)
		return UpdateConsortium500TextResponse("Internal server error"), nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err, "operation", "UpdateConsortium")
		return UpdateConsortium500TextResponse("Internal server error"), nil
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	err = qtx.UpdateConsortium(ctx, db.UpdateConsortiumParams{
		ID:    request.Id,
		Name:  derefOrDefault(request.Body.Name, orig.Name),
		Entry: maybeUpdateCol(orig.Entry, request.Body.Entry),
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to update consortium", "error", err, "id", request.Id)
		return UpdateConsortium500TextResponse("Internal server error"), nil
	}
	err = tx.Commit(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err, "operation", "UpdateConsortium")
		return UpdateConsortium500TextResponse("Internal server error"), nil
	}

	return UpdateConsortium204Response{}, nil
}

func (a ApiImpl) DeleteConsortium(ctx context.Context, request DeleteConsortiumRequestObject) (DeleteConsortiumResponseObject, error) {
	err := a.queries.DeleteConsortium(ctx, request.Id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete consortium", "error", err, "id", request.Id)
		return DeleteConsortium500TextResponse("Internal server error"), nil
	}
	return DeleteConsortium204Response{}, nil
}
