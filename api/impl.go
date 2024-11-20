package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"indexdata/directoryish/db"
)

type Server struct {
	ctx     context.Context
	queries *db.Queries
}

func NewServer(queries *db.Queries, ctx context.Context) Server {
	return Server{queries: queries, ctx: ctx}
}

func (s Server) GetEntries(w http.ResponseWriter, r *http.Request, params GetEntriesParams) {
	var resp []Entry

	dbentries, err := s.queries.ListEntries(s.ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(dbentries)

	for _, dbentry := range dbentries {
		resp = append(resp, Entry{
			Name: dbentry.Name,
		})
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (Server) GetEntryByID(w http.ResponseWriter, r *http.Request, id int64) {
}

func (Server) AddEntry(w http.ResponseWriter, r *http.Request) {
}

func (Server) DeleteEntry(w http.ResponseWriter, r *http.Request, id int64) {
}
