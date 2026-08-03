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
