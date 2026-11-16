package common

import dirapi "github.com/indexdata/crosslink/directory/api"

func IllConfigBool(entry dirapi.Entry, fallback bool, field func(dirapi.IllConfig) *bool) bool {
	if entry.IllConfig != nil {
		if value := field(*entry.IllConfig); value != nil {
			return *value
		}
	}
	return fallback
}

func IllConfigString(entry dirapi.Entry, fallback string, field func(dirapi.IllConfig) *string) string {
	if entry.IllConfig != nil {
		if value := field(*entry.IllConfig); value != nil {
			return *value
		}
	}
	return fallback
}

// IllConfigCodeDescription returns the description configured for code in the
// list selected by field, or "" when the entry has no such list or code.
// Libraries define their own vocabularies, so codes are matched exactly.
func IllConfigCodeDescription(entry dirapi.Entry, code string, field func(dirapi.IllConfig) *[]dirapi.IllConfigCodeDescription) string {
	if entry.IllConfig == nil || code == "" {
		return ""
	}
	list := field(*entry.IllConfig)
	if list == nil {
		return ""
	}
	for _, item := range *list {
		if item.Code == code {
			return item.DefaultDesc
		}
	}
	return ""
}
