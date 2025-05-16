package vcs

import (
	_ "embed"
	"runtime/debug"
)

//go:embed commit.txt
var CommitId string

func GetCommit() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return CommitId
}
