package buildinfo

import "runtime"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func GoVersion() string { return runtime.Version() }
