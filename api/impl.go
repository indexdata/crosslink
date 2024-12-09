package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"indexdata/directoryish/db"
)

type Server struct {
	pool    *pgxpool.Pool
	ctx     context.Context
	queries *db.Queries
}

func NewServer(pool *pgxpool.Pool, queries *db.Queries, ctx context.Context) Server {
	return Server{pool: pool, queries: queries, ctx: ctx}
}

func (s Server) GetEntries(w http.ResponseWriter, r *http.Request, params GetEntriesParams) {
	var resp []Entry

	rows, err := s.queries.ListEntries(s.ctx)
	if err != nil {
		log.Fatal(err)
	}

	for _, row := range rows {
		last := len(resp) - 1

		if last < 0 || resp[last].Id != row.DirectoryEntry.ID {
			resp = append(resp, Entry{
				Id:   row.DirectoryEntry.ID,
				Name: row.DirectoryEntry.Name,
			})
			last++
		}

		if resp[last].Symbols == nil {
			s := []Symbol{}
			resp[last].Symbols = &s
		}

		*resp[last].Symbols = append(*resp[last].Symbols, Symbol{
			Id:     row.Symbol.ID,
			Symbol: &row.Symbol.Symbol,
		})
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (Server) GetEntryByID(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
}

func (s Server) AddEntry(w http.ResponseWriter, r *http.Request) {
	var newEntry NewEntry
	if err := json.NewDecoder(r.Body).Decode(&newEntry); err != nil {
		log.Fatal(err)
		return
	}

	tx, err := s.pool.Begin(s.ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(s.ctx)
	qtx := s.queries.WithTx(tx)

	toInsert := db.CreateEntryParams{
		Name:         newEntry.Name,
		ContactName:  pgtype.Text{String: *newEntry.ContactName, Valid: true},
		EmailAddress: pgtype.Text{String: *newEntry.Email, Valid: true},
	}
	insertedEntry, err := qtx.CreateEntry(s.ctx, toInsert)
	if err != nil {
		log.Fatal(err)
	}

	for _, symbol := range *newEntry.Symbols {
		auth, err := qtx.AuthorityBySymbol(s.ctx, strings.ToUpper(*symbol.Authority))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Println("Unrecognized authority")
			}
			log.Fatal(err)
		}

		_, err = qtx.CreateSymbol(s.ctx, db.CreateSymbolParams{
			Owner:     insertedEntry.ID,
			Symbol:    strings.ToUpper(*symbol.Symbol),
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

	var resp Entry
	resp.Id = insertedEntry.ID
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)

	tx.Commit(s.ctx)
	if err != nil {
		log.Fatal(err)
	}
}

func (Server) DeleteEntry(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
}
