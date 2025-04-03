package adapter

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error)
}

type DirectoryLookupParams struct {
	Symbols []string
}

type DirectoryEntry struct {
	Symbol           []string
	Name             string
	URL              string
	Vendor           string
	CustomProperties map[string]interface{}
}
