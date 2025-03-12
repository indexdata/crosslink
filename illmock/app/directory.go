package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/illmock/directory"
)

var _ directory.StrictServerInterface = (*DirectoryMock)(nil)

type DirectoryMock struct{}

func (d *DirectoryMock) GetEntries(ctx context.Context, request directory.GetEntriesRequestObject) (directory.GetEntriesResponseObject, error) {
	log.Info("GetEntries ", "cql", request.Params.Cql, "limit", request.Params.Limit, "offset", request.Params.Offset)

	var entries directory.GetEntries200JSONResponse

	id := uuid.New()
	symbols := []directory.Symbol{
		{
			Symbol: "sym1",
		},
	}
	entry := directory.Entry{
		Name:    "diku",
		Id:      &id,
		Symbols: &symbols,
	}
	entries = append(entries, entry)
	return entries, nil
}

func NewDirectoryMock() *DirectoryMock {
	return &DirectoryMock{}
}
