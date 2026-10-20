package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

type builtinProfile struct {
	LMS     object `json:"lmsConfig"`
	Catalog object `json:"catalogConfig"`
}

// Cache definitions and errors alike: the bundled specification cannot change
// during this process. Resolve must never modify these shared defaults.
var loadBuiltins = sync.OnceValues(func() (map[string]builtinProfile, error) {
	data, err := dirapi.GetSpecJSON()
	if err != nil {
		return nil, fmt.Errorf("load bundled directory specification: %w", err)
	}
	return decodeBuiltins(data)
})

func decodeBuiltins(data []byte) (map[string]builtinProfile, error) {
	var schema struct {
		Profiles   map[string]map[string]json.RawMessage `json:"x-host-profiles"`
		Components struct {
			Schemas map[string]struct {
				Enum []string `json:"enum"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode directory specification: %w", err)
	}
	if len(schema.Profiles) == 0 {
		return nil, fmt.Errorf("directory specification: missing or empty x-host-profiles")
	}
	for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		if schema.Profiles[name] == nil {
			return nil, fmt.Errorf("host profile %s: missing definition", name)
		}
	}
	vendors := schema.Components.Schemas["HostLmsVendor"].Enum
	profiles := make(map[string]builtinProfile, len(schema.Profiles))
	for _, name := range slices.Sorted(maps.Keys(schema.Profiles)) {
		if !slices.Contains(vendors, name) {
			return nil, fmt.Errorf("host profile %s: not in HostLmsVendor enum", name)
		}
		raw := schema.Profiles[name]
		// JSON struct decoding accepts case variants, but profile sections must
		// use their canonical names, including when both spellings are present.
		for _, section := range slices.Sorted(maps.Keys(raw)) {
			if section != "lmsConfig" && section != "catalogConfig" {
				return nil, fmt.Errorf("host profile %s: invalid section %q; allowed sections are lmsConfig and catalogConfig", name, section)
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("host profile %s: encode definition: %w", name, err)
		}
		var config struct {
			LMS     *dirapi.LmsConfig     `json:"lmsConfig"`
			Catalog *dirapi.CatalogConfig `json:"catalogConfig"`
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return nil, fmt.Errorf("host profile %s: validate definition: %w", name, err)
		}
		// Retain raw maps rather than re-encoding the typed configuration, which
		// could materialize omitted fields or omit explicit zero values.
		var profile builtinProfile
		if err := json.Unmarshal(encoded, &profile); err != nil {
			return nil, fmt.Errorf("host profile %s: decode definition: %w", name, err)
		}
		profiles[name] = profile
	}
	return profiles, nil
}
