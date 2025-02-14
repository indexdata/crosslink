package api

import (
	"context"
	"log"
)

func (a ApiImpl) AddAuthority(ctx context.Context, request AddAuthorityRequestObject) (AddAuthorityResponseObject, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	insertedAuthority, err := qtx.CreateAuthority(ctx, request.Body.Symbol)
	if err != nil {
		log.Println(err)
		return AddAuthority400TextResponse("Error creating authority"), nil
	}

	var resp Id
	resp.Id = insertedAuthority.ID

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return AddAuthority200JSONResponse(resp), nil
}

func (a ApiImpl) GetAuthorities(ctx context.Context, request GetAuthoritiesRequestObject) (GetAuthoritiesResponseObject, error) {
	rows, err := a.queries.ListAuthorities(ctx)
	if err != nil {
		log.Fatal(err)
	}

	resp := make([]Authority, 0, len(rows))

	for _, row := range rows {
		resp = append(resp, Authority{
			Id:     &row.ID,
			Symbol: row.Symbol,
		})
	}

	return GetAuthorities200JSONResponse(resp), nil
}
