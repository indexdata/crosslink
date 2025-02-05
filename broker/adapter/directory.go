package adapter

import (
	"errors"
	"github.com/indexdata/go-utils/utils"
	"strings"
)

var MOCK_CLIENT_URL = utils.GetEnv("MOCK_CLIENT_URL", "http://localhost:19083/iso18626")

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error)
}

type DirectoryLookupParams struct {
	Symbols []string
}

type DirectoryEntry struct {
	Symbol string
	URL    string
}

type MockDirectoryLookupAdapter struct {
}

func (m *MockDirectoryLookupAdapter) Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error) {
	if strings.Contains(params.Symbols[0], "error") {
		return []DirectoryEntry{}, errors.New("there is error")
	}
	if strings.Contains(params.Symbols[0], "d-not-found") {
		return []DirectoryEntry{}, nil
	}

	var dirs []DirectoryEntry
	for _, value := range params.Symbols {
		dirs = append(dirs, DirectoryEntry{
			Symbol: value,
			URL:    MOCK_CLIENT_URL,
		})
	}
	return dirs, nil
}
