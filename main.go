// Command tracex is the TraceX command line: sign in, connect a repository and
// ask TraceX about your systems from the terminal.
package main

import (
	"os"

	"github.com/tracexpro/tracex-cli/internal/cli"
)

// Set at build time: -ldflags "-X main.version=v0.1.0 -X main.commit=abc123"
var (
	version = "dev"
	commit  = ""
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Build{Version: version, Commit: commit}))
}
