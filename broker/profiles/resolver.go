package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

// Resolver holds an immutable snapshot of Directory host profiles. It is safe
// for concurrent use; resolving an entry never changes the snapshot.
type Resolver struct {
	definitions map[string]hostProfile
}

type hostProfile struct {
	LMS     object `json:"lmsConfig"`
	Catalog object `json:"catalogConfig"`
}

// NewResolver loads and validates x-host-profiles from a Directory OpenAPI JSON
// document. All currently supported profiles must be present.
func NewResolver(specJSON []byte) (*Resolver, error) {
	var document struct {
		Profiles map[string]json.RawMessage `json:"x-host-profiles"`
	}
	if err := json.Unmarshal(specJSON, &document); err != nil {
		return nil, fmt.Errorf("decode Directory specification: %w", err)
	}
	if len(document.Profiles) == 0 {
		return nil, fmt.Errorf("Directory specification has no x-host-profiles")
	}
	resolver := &Resolver{definitions: make(map[string]hostProfile, len(document.Profiles))}
	for name, data := range document.Profiles {
		profile, err := decodeProfile(data)
		if err != nil {
			return nil, fmt.Errorf("host profile %s: %w", name, err)
		}
		resolver.definitions[name] = profile
	}
	for _, name := range []string{"Generic", "Alma", "FOLIO", "Koha", "Sierra"} {
		if _, ok := resolver.definitions[name]; !ok {
			return nil, fmt.Errorf("x-host-profiles is missing required profile %s", name)
		}
		lms := &dirapi.LmsConfig{}
		lms.Vendor.Set(name)
		catalog := &dirapi.CatalogConfig{}
		catalog.Profile.Set(name)
		if _, err := resolver.Resolve(dirapi.Entry{LmsConfig: lms, CatalogConfig: catalog}); err != nil {
			return nil, fmt.Errorf("validate host profile %s: %w", name, err)
		}
	}
	return resolver, nil
}

func decodeProfile(data []byte) (hostProfile, error) {
	// Validate names and types against the Directory API, including nested fields.
	var config struct {
		LMS     *dirapi.LmsConfig     `json:"lmsConfig"`
		Catalog *dirapi.CatalogConfig `json:"catalogConfig"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return hostProfile{}, err
	}
	if config.LMS == nil || config.Catalog == nil {
		return hostProfile{}, fmt.Errorf("lmsConfig and catalogConfig must be objects")
	}
	var profile hostProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return hostProfile{}, err
	}
	return profile, nil
}
