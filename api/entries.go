package api

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"indexdata/directoryish/db"
)

func addRowToEntry(row db.ListEntriesRow, entry *Entry) {
	if row.Entrysymbol.ID != nil {
		if entry.Symbols == nil {
			s := []Symbol{}
			entry.Symbols = &s
		}

		if !elementHasProperty(*entry.Symbols, "Id", *row.Entrysymbol.ID) {
			*entry.Symbols = append(*entry.Symbols, Symbol{
				Id:        row.Entrysymbol.ID,
				Symbol:    *row.Entrysymbol.Symbol,
				Authority: *row.Entrysymbol.Authority,
			})
		}
	}

	if row.Entryendpoint.ID != nil {
		if entry.Endpoints == nil {
			s := []ServiceEndpoint{}
			entry.Endpoints = &s
		}

		if !elementHasProperty(*entry.Endpoints, "Id", *row.Entryendpoint.ID) {
			*entry.Endpoints = append(*entry.Endpoints, ServiceEndpoint{
				Id:      row.Entryendpoint.ID,
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

		if last < 0 || !bytes.Equal(resp[last].Id[:], (&row.Entry.ID)[:]) {
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

func (a ApiImpl) GetEntry(ctx context.Context, request GetEntryRequestObject) (GetEntryResponseObject, error) {
	var rows []db.ListEntriesRow
	var err error
	if request.Key == GetEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetEntry400TextResponse("Error parsing id"), nil
		}
		rows, err = a.queries.ListEntries(ctx, db.ListEntriesParams{ID: &parsedId})
	} else if request.Key == GetEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		rows, err = a.queries.ListEntries(ctx, db.ListEntriesParams{Authority: &authority, Symbol: &symbol})
	}
	if err != nil {
		log.Fatal(err)
	}

	if len(rows) == 0 {
		return GetEntry404TextResponse("Entry not found"), nil
	}

	return GetEntry200JSONResponse(getOneEntryFromRows(rows)), nil
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
			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				Owner:     insertedEntry.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: strings.ToUpper(symbol.Authority),
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						return AddEntry400TextResponse("Duplicate symbol"), nil
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

	return AddEntry201JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {
	var orig db.Entry
	var err error
	if request.Key == UpdateEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("Error parsing id"), nil
		}
		orig, err = a.queries.EntryById(ctx, parsedId)
		if err != nil {
			print(err.Error())
		}
	} else if request.Key == UpdateEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		orig, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
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
		ContactName: maybeUpdateCol(orig.ContactName, request.Body.ContactName),
		Email:       maybeUpdateCol(orig.Email, request.Body.Email),
		ID:          orig.ID,
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
			qtx.DeleteOtherOwnedSymbols(ctx, db.DeleteOtherOwnedSymbolsParams{Owner: orig.ID, Ids: patchedSymbols})
		} else {
			qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
		}

		// Update/create symbols
		for _, symbol := range reqsyms {
			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				ID:        symbol.Id,
				Owner:     orig.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: strings.ToUpper(symbol.Authority),
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
		qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
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
			qtx.DeleteOtherOwnedServiceEndpoints(ctx, db.DeleteOtherOwnedServiceEndpointsParams{Entry: orig.ID, Ids: patchedEndpoints})
		} else {
			qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
		}

		// Update/create endpoints
		for _, endpoint := range reqeps {
			_, err = qtx.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{
				ID:      endpoint.Id,
				Entry:   orig.ID,
				Name:    endpoint.Name,
				Type:    endpoint.Type,
				Address: endpoint.Address,
			})
			if err != nil {
				log.Fatal(err)
			}
		}
	} else if request.Body.Endpoints.IsNull() {
		qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
	}

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return UpdateEntry204Response{}, nil
}

func (a ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	var err error
	if request.Key == DeleteEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteEntry400TextResponse("Error parsing id"), nil
		}
		err = a.queries.DeleteEntryById(ctx, parsedId)
	} else if request.Key == DeleteEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		err = a.queries.DeleteEntryBySymbol(ctx, db.DeleteEntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		log.Fatal(err)
	}
	return DeleteEntry204Response{}, nil
}
