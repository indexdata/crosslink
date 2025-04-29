package adapter

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	DirectoryAdapter string = "DIRECTORY_ADAPTER"
	DirectoryApiUrl  string = "DIRECTORY_API_URL"
)

func CreateDirectoryLookupAdapter(cfg map[string]string) (DirectoryLookupAdapter, error) {
	directoryAdapterVal, ok := cfg[DirectoryAdapter]
	if !ok {
		return nil, fmt.Errorf("missing value for %s", DirectoryAdapter)
	}
	if directoryAdapterVal == "api" {
		apiUrlsVal, ok := cfg[DirectoryApiUrl]
		if !ok {
			return nil, fmt.Errorf("missing value for %s", DirectoryApiUrl)
		}
		return CreateApiDirectory(http.DefaultClient, strings.Split(apiUrlsVal, ",")), nil
	}
	if directoryAdapterVal == "mock" {
		return &MockDirectoryLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", DirectoryAdapter)
}
