// Package storedconfig defines how directory entries store and update local
// configuration in the entries.configs JSON column. Each stored container is
// the JSON object returned when reading the entry without inheritance.
package storedconfig

import "encoding/json"

// Configs maps API property names such as lmsConfig to local configuration
// objects. A missing key means the entry has no local container of that name.
type Configs map[string]json.RawMessage
