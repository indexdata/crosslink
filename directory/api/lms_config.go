package api

import (
	"encoding/json"

	"github.com/oapi-codegen/nullable"
)

func patronProfilesJSON(profiles *PatronProfiles) []byte {
	if profiles == nil {
		return nil
	}
	value, _ := json.Marshal(*profiles)
	return value
}

func maybeUpdatePatronProfiles(current []byte, patch nullable.Nullable[PatronProfiles]) []byte {
	if !patch.IsSpecified() {
		return current
	}
	if patch.IsNull() {
		return nil
	}
	profiles := patch.MustGet()
	return patronProfilesJSON(&profiles)
}
