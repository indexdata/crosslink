// Package storedconfig holds the stored form of the configuration containers
// of a directory entry, and the rules for writing them. An entry stores its
// containers as one JSON object in entries.configs. Each container in it is
// the JSON object that entry reads return.
package storedconfig

import "encoding/json"

// Configs is the configuration containers an entry stores locally. It maps a
// container's API property name, such as lmsConfig, to the stored JSON
// object. A container the entry does not store is absent.
type Configs map[string]json.RawMessage
