package api

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"indexdata/directoryish/db"
)

func addRowToEntry(row db.ListEntriesRow, entry *Entry) {
	if row.Entrysymbol.ID.Valid {
		if entry.Symbols == nil {
			s := []Symbol{}
			entry.Symbols = &s
		}

		symid, _ := uuid.FromBytes(row.Entrysymbol.ID.Bytes[:])

		if !elementHasProperty(*entry.Symbols, "Id", symid) {
			*entry.Symbols = append(*entry.Symbols, Symbol{
				Id:        &symid,
				Symbol:    *row.Entrysymbol.Symbol,
				Authority: *row.SymbolAuthority,
			})
		}
	}

	if row.Entryendpoint.ID.Valid {
		if entry.Endpoints == nil {
			s := []ServiceEndpoint{}
			entry.Endpoints = &s
		}

		epid, _ := uuid.FromBytes(row.Entryendpoint.ID.Bytes[:])

		if !elementHasProperty(*entry.Endpoints, "Id", epid) {
			*entry.Endpoints = append(*entry.Endpoints, ServiceEndpoint{
				Id:      &epid,
				Name:    *row.Entryendpoint.Name,
				Type:    *row.Entryendpoint.Type,
				Address: *row.Entryendpoint.Address,
			})
		}
	}
}

func (a ApiImpl) GetEntries(ctx context.Context, request GetEntriesRequestObject) (GetEntriesResponseObject, error) {
	rows, err := a.queries.ListEntries(ctx, db.ListEntriesParams{})
	if err != nil {
		log.Fatal(err)
	}

	// Need to initialise resp as explicitly zero length because a simple
	// var resp []Entry will be JSON-encoded as null rather than [].
	// See https://github.com/golang/go/issues/27589
	// ...so may as well allocate an appropriate capacity while we're at it
	resp := make([]Entry, 0, len(rows))

	for _, row := range rows {
		last := len(resp) - 1

		if last < 0 || resp[last].Id != &row.Entry.ID {
			resp = append(resp, Entry{
				Id:          &row.Entry.ID,
				Name:        row.Entry.Name,
				ContactName: row.Entry.ContactName,
				Email:       row.Entry.Email,
			})
			last++
		}

		addRowToEntry(row, &resp[last])
	}

	return GetEntries200JSONResponse(resp), nil
}

func getOneEntryFromRows(rows []db.ListEntriesRow) Entry {
	var resp = Entry{
		Id:          &rows[0].Entry.ID,
		Name:        rows[0].Entry.Name,
		ContactName: rows[0].Entry.ContactName,
		Email:       rows[0].Entry.Email,
	}

	for _, row := range rows {
		addRowToEntry(row, &resp)
	}

	return resp
}

func (a ApiImpl) GetEntryByID(ctx context.Context, request GetEntryByIDRequestObject) (GetEntryByIDResponseObject, error) {
	rows, err := a.queries.ListEntries(ctx, db.ListEntriesParams{ID: pgtype.UUID{Bytes: request.Id, Valid: true}})
	if err != nil {
		log.Fatal(err)
	}

	if len(rows) == 0 {
		return GetEntryByID404TextResponse("Entry not found"), nil
	}

	return GetEntryByID200JSONResponse(getOneEntryFromRows(rows)), nil
}

func (a ApiImpl) GetEntryBySymbol(ctx context.Context, request GetEntryBySymbolRequestObject) (GetEntryBySymbolResponseObject, error) {
	authority, symbol, err := resolveCombinedSymbol(request.Symbol)
	if err != nil {
		return GetEntryBySymbol400TextResponse("No delimiter found in symbol"), nil
	}

	rows, err := a.queries.ListEntries(ctx, db.ListEntriesParams{Authority: &authority, Symbol: &symbol})
	if err != nil {
		log.Fatal(err)
	}

	if len(rows) == 0 {
		return GetEntryBySymbol404TextResponse("Entry not found"), nil
	}

	return GetEntryBySymbol200JSONResponse(getOneEntryFromRows(rows)), nil
}

func (a ApiImpl) AddEntry(ctx context.Context, request AddEntryRequestObject) (AddEntryResponseObject, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	toInsert := db.CreateEntryParams{
		Name:        request.Body.Name,
		ContactName: request.Body.ContactName,
		Email:       request.Body.Email,
	}
	insertedEntry, err := qtx.CreateEntry(ctx, toInsert)
	if err != nil {
		log.Fatal(err)
	}

	if request.Body.Symbols != nil {
		for _, symbol := range *request.Body.Symbols {
			auth, err := qtx.AuthorityBySymbol(ctx, strings.ToUpper(symbol.Authority))
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return AddEntry400TextResponse("Unrecognized authority"), nil
				}
			}

			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				Owner:     insertedEntry.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: auth.ID,
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						log.Println("Duplicate symbol")
					}
				}
				log.Fatal(err)
			}
		}
	}

	if request.Body.Endpoints != nil {
		for _, endpoint := range *request.Body.Endpoints {
			_, err = qtx.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{
				Entry:   insertedEntry.ID,
				Name:    endpoint.Name,
				Type:    endpoint.Type,
				Address: endpoint.Address,
			})
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	var resp Id
	resp.Id = insertedEntry.ID

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return AddEntry200JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {
	var orig db.Entry
	orig, err := a.queries.EntryById(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateEntry404TextResponse("Entry not found"), nil
	} else if err != nil {
		log.Fatal(err)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	err = qtx.UpdateEntry(ctx, db.UpdateEntryParams{
		Name:        derefOrDefault(request.Body.Name, orig.Name),
		ContactName: maybeUpdateTxtCol(orig.ContactName, request.Body.ContactName),
		Email:       maybeUpdateTxtCol(orig.Email, request.Body.Email),
		ID:          request.Id,
	})
	if err != nil {
		log.Fatal(err)
	}

	if request.Body.Symbols.IsSpecified() && !request.Body.Symbols.IsNull() {
		reqsyms := request.Body.Symbols.MustGet()
		// Delete existing symbols not present
		var patchedSymbols []uuid.UUID
		for _, symbol := range reqsyms {
			if symbol.Id != nil {
				patchedSymbols = append(patchedSymbols, *symbol.Id)
			}
		}
		if len(patchedSymbols) > 0 {
			qtx.DeleteOtherOwnedSymbols(ctx, db.DeleteOtherOwnedSymbolsParams{Owner: request.Id, Ids: patchedSymbols})
		} else {
			qtx.DeleteAllOwnedSymbols(ctx, request.Id)
		}

		// Update/create symbols
		for _, symbol := range reqsyms {
			auth, err := qtx.AuthorityBySymbol(ctx, strings.ToUpper(symbol.Authority))
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return UpdateEntry400TextResponse("Unrecognized authority"), nil
				}
			}

			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				ID:        symbol.Id,
				Owner:     request.Id,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: auth.ID,
			})
			if err != nil {
				log.Println(err)
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						return UpdateEntry400TextResponse("Duplicate symbol"), nil
					}
				}
				log.Fatal(err)
			}
		}
	} else if request.Body.Symbols.IsNull() {
		qtx.DeleteAllOwnedSymbols(ctx, request.Id)
	}

	if request.Body.Endpoints.IsSpecified() && !request.Body.Endpoints.IsNull() {
		reqeps := request.Body.Endpoints.MustGet()
		// Delete existing endpoints not present
		var patchedEndpoints []uuid.UUID
		for _, endpoint := range reqeps {
			if endpoint.Id != nil {
				patchedEndpoints = append(patchedEndpoints, *endpoint.Id)
			}
		}
		if len(patchedEndpoints) > 0 {
			qtx.DeleteOtherOwnedServiceEndpoints(ctx, db.DeleteOtherOwnedServiceEndpointsParams{Entry: request.Id, Ids: patchedEndpoints})
		} else {
			qtx.DeleteAllOwnedServiceEndpoints(ctx, request.Id)
		}

		// Update/create endpoints
		for _, endpoint := range reqeps {
			_, err = qtx.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{
				ID:      endpoint.Id,
				Entry:   request.Id,
				Name:    endpoint.Name,
				Type:    endpoint.Type,
				Address: endpoint.Address,
			})
			if err != nil {
				log.Fatal(err)
			}
		}
	} else if request.Body.Endpoints.IsNull() {
		qtx.DeleteAllOwnedServiceEndpoints(ctx, request.Id)
	}

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return UpdateEntry204Response{}, nil
}

func (ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	return DeleteEntry204Response{}, nil
}
