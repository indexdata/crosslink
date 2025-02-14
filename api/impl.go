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
	"github.com/jackc/pgx/v5/pgxpool"

	"indexdata/directoryish/db"
)

type ApiImpl struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Make sure we conform to StrictServerInterface
var _ StrictServerInterface = (*ApiImpl)(nil)

func NewApiImpl(pool *pgxpool.Pool, queries *db.Queries) ApiImpl {
	return ApiImpl{pool: pool, queries: queries}
}

func addRowToEntry(row db.ListEntriesRow, entry *Entry) {
	if row.Entrysymbol.ID.Valid {
		if entry.Symbols == nil {
			s := []Symbol{}
			entry.Symbols = &s
		}

		symid, _ := uuid.FromBytes(row.Entrysymbol.ID.Bytes[:])

		*entry.Symbols = append(*entry.Symbols, Symbol{
			Id:        &symid,
			Symbol:    *row.Entrysymbol.Symbol,
			Authority: *row.SymbolAuthority,
		})
	}
}

func (a ApiImpl) GetEntries(ctx context.Context, request GetEntriesRequestObject) (GetEntriesResponseObject, error) {
	rows, err := a.queries.ListEntries(ctx, pgtype.UUID{})
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

func (a ApiImpl) GetEntryByID(ctx context.Context, request GetEntryByIDRequestObject) (GetEntryByIDResponseObject, error) {
	rows, err := a.queries.ListEntries(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		log.Fatal(err)
	}

	if len(rows) == 0 {
		return GetEntryByID404TextResponse("Entry not found"), nil
	}

	var resp = Entry{
		Id:          &rows[0].Entry.ID,
		Name:        rows[0].Entry.Name,
		ContactName: rows[0].Entry.ContactName,
		Email:       rows[0].Entry.Email,
	}

	for _, row := range rows {
		addRowToEntry(row, &resp)
	}

	return GetEntryByID200JSONResponse(resp), nil
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

			_, err = qtx.CreateSymbol(ctx, db.CreateSymbolParams{
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

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return UpdateEntry204Response{}, nil
}

func (ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	return DeleteEntry204Response{}, nil
}

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
