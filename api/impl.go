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
	"github.com/oapi-codegen/nullable"

	"indexdata/directoryish/db"
)

func NlblToPGTxt(nlbl nullable.Nullable[string]) pgtype.Text {
	if nlbl.IsNull() || !nlbl.IsSpecified() {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: nlbl.MustGet(), Valid: true}
}

func PtrToPGTxt(ptr *string) pgtype.Text {
	if ptr == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *ptr, Valid: true}
}

func PGTxtToNlbl(pgtxt pgtype.Text) nullable.Nullable[string] {
	if !pgtxt.Valid {
		nlbl := nullable.NewNullNullable[string]()
		nlbl.SetUnspecified() // We don't store explicitly null strings
		return nlbl
	}
	return nullable.NewNullableWithValue(pgtxt.String)
}

type ApiImpl struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Make sure we conform to StrictServerInterface
var _ StrictServerInterface = (*ApiImpl)(nil)

func NewApiImpl(pool *pgxpool.Pool, queries *db.Queries) ApiImpl {
	return ApiImpl{pool: pool, queries: queries}
}

func (a ApiImpl) GetEntries(ctx context.Context, request GetEntriesRequestObject) (GetEntriesResponseObject, error) {
	rows, err := a.queries.ListEntries(ctx)
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

		if last < 0 || resp[last].Id != row.Entry.ID {
			resp = append(resp, Entry{
				Id:    row.Entry.ID,
				Name:  row.Entry.Name,
				Email: PGTxtToNlbl(row.Entry.Email),
			})
			last++
		}

		if row.Entrysymbol.ID.Valid {
			if resp[last].Symbols == nil {
				s := []Symbol{}
				resp[last].Symbols = &s
			}

			symid, _ := uuid.FromBytes(row.Entrysymbol.ID.Bytes[:])

			*resp[last].Symbols = append(*resp[last].Symbols, Symbol{
				Id:     symid,
				Symbol: row.Entrysymbol.Symbol.String,
			})
		}
	}

	return GetEntries200JSONResponse(resp), nil
}

func (a ApiImpl) GetEntryByID(ctx context.Context, request GetEntryByIDRequestObject) (GetEntryByIDResponseObject, error) {
	var resp Entry
	entry, err := a.queries.EntryById(ctx, request.Id)
	if err != nil {
		log.Fatal(err)
	}

	resp.Id = entry.ID
	resp.Name = entry.Name
	resp.ContactName = PGTxtToNlbl(entry.ContactName)
	resp.Email = PGTxtToNlbl(entry.Email)

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
		ContactName: NlblToPGTxt(request.Body.ContactName),
		Email:       NlblToPGTxt(request.Body.Email),
	}
	insertedEntry, err := qtx.CreateEntry(ctx, toInsert)
	if err != nil {
		log.Fatal(err)
	}

	if request.Body.Symbols != nil {
		for _, symbol := range *request.Body.Symbols {
			auth, err := qtx.AuthorityBySymbol(ctx, strings.ToUpper(*symbol.Authority))
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Println("Unrecognized authority")
				}
				log.Fatal(err)
			}

			_, err = qtx.CreateSymbol(ctx, db.CreateSymbolParams{
				Owner:     insertedEntry.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: auth.ID,
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" {
						log.Println("Duplicate symbol")
					}
				}
				log.Fatal(err)
			}
		}
	}

	var resp Id
	resp.Id = insertedEntry.ID

	tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return AddEntry200JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	err = qtx.UpdateEntry(ctx, db.UpdateEntryParams{
		Name:           PtrToPGTxt(request.Body.Name),
		ContactName:    NlblToPGTxt(request.Body.ContactName),
		DelContactName: request.Body.ContactName.IsNull(),
		Email:          NlblToPGTxt(request.Body.Email),
		DelEmail:       request.Body.Email.IsNull(),
		ID:             request.Id,
	})
	if err != nil {
		log.Fatal(err)
	}

	tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return UpdateEntry204Response{}, nil
}

func (ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	var resp DeleteEntryResponseObject
	return resp, nil
}
