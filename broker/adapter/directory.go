package adapter

import (
	"errors"
	"strings"
)

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
			URL:    "http://localhost:19082/iso18626",
		})
	}
	if len(dirs) == 0 {
		dirs = append(dirs, DirectoryEntry{
			Symbol: "isil:resp",
			URL:    "http://localhost:19082/iso18626",
		})
	}
	return dirs, nil
}
