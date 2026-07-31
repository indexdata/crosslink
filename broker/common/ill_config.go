package common

import "github.com/indexdata/crosslink/directory"

func IllConfigBool(entry directory.Entry, fallback bool, field func(directory.IllConfig) *bool) bool {
	if entry.IllConfig != nil {
		if value := field(*entry.IllConfig); value != nil {
			return *value
		}
	}
	return fallback
}

func IllConfigString(entry directory.Entry, fallback string, field func(directory.IllConfig) *string) string {
	if entry.IllConfig != nil {
		if value := field(*entry.IllConfig); value != nil {
			return *value
		}
	}
	return fallback
}
