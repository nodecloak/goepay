package systemupdate

import (
	"runtime"
	"runtime/debug"
)

// These are populated by the reproducible image build, never by a web request.
var Version = "development"
var SourceSHA string
var Commit string

func RuntimeBuild(schema int64) Build {
	b := Build{Version: Version, SourceSHA: SourceSHA, Commit: RuntimeCommit(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Schema: schema}
	return b
}
func RuntimeCommit() string {
	if Commit != "" {
		return Commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}
