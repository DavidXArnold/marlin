package main

import (
	"github.com/DavidXArnold/marlin/cmd"
)

// goreleaser injects these via ldflags at release time.
var (
	version = "0.0.1"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
