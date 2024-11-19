package api

import (
	"encoding/json"
	"net/http"
)

type Server struct{}

func NewServer() Server {
	return Server{}
}

func (Server) GetEntries(w http.ResponseWriter, r *http.Request, params GetEntriesParams) {
	resp := Entry{
		Name: "yay",
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
