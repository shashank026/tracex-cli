// Package cli implements the tracex commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/config"
	"github.com/shashank026/tracex-cli/internal/ui"
)

// Build identifies this binary.
type Build struct {
	Version string
	Commit  string
}

const usage = `TraceX in your terminal: ask about your data, code and recent changes.

Usage:
  tracex -c [question]     Chat with TraceX (one question, or a session without one)
  tracex login             Sign in through your browser
  tracex logout            Sign this computer out
  tracex init              Link this git repository to a TraceX project
  tracex status            Who is signed in and which project this folder uses
  tracex version           Print the version

Chat options:
  -p, --project <id>       Ask this project instead of the linked one

Environment:
  TRACEX_API_URL           TraceX server (default https://app.tracexpro.com)
  NO_COLOR                 Plain output without colors
`

// Run executes one command line and returns the exit code.
func Run(args []string, build Build) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return 0
	}
	ctx := context.Background()
	var err error
	switch command, rest := args[0], args[1:]; command {
	case "version", "--version", "-v":
		printVersion(build)
	case "help", "--help", "-h":
		fmt.Print(usage)
	case "login":
		err = login(ctx, build, rest)
	case "logout":
		err = logout(ctx, build)
	case "init":
		err = initRepo(ctx, build, rest)
	case "status", "whoami":
		err = status(build)
	case "-c", "--chat", "chat", "ask":
		err = chat(ctx, build, rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n%s", command, usage)
		return 2
	}
	if err != nil {
		if !errors.Is(err, errQuiet) {
			ui.Fail("%s", err)
		}
		return 1
	}
	return 0
}

// errQuiet ends a command whose problem was already printed.
var errQuiet = errors.New("")

func printVersion(build Build) {
	line := "tracex " + build.Version
	if build.Commit != "" {
		line += " (" + build.Commit + ")"
	}
	fmt.Printf("%s %s/%s\n", line, runtime.GOOS, runtime.GOARCH)
}

// signedIn is a client for the saved sign-in.
func signedIn(build Build) (*api.Client, *config.Credentials, error) {
	creds, err := config.LoadCredentials()
	if err != nil {
		return nil, nil, err
	}
	return api.New(config.APIURL(creds), creds.AccessToken, build.Version), creds, nil
}

func status(build Build) error {
	creds, err := config.LoadCredentials()
	if err != nil {
		return err
	}
	fmt.Printf("%s %s %s\n", ui.Green("✓"), ui.Bold(creds.Email), ui.Dim("in "+creds.Organization))
	fmt.Printf("  %s %s\n", ui.Dim("Server: "), config.APIURL(creds))
	fmt.Printf("  %s %s\n", ui.Dim("Expires:"), creds.ExpiresAt.Local().Format("2 Jan 2006 15:04"))
	cwd, _ := os.Getwd()
	if project, path, _ := config.FindProject(cwd); project != nil {
		fmt.Printf("  %s %s %s\n", ui.Dim("Project:"), project.ProjectName, ui.Dim("("+path+")"))
	} else if creds.ProjectName != "" {
		fmt.Printf("  %s %s %s\n", ui.Dim("Project:"), creds.ProjectName, ui.Dim("(default; run tracex init in a repository to link it)"))
	}
	return nil
}

// flag returns the value of -x/--long in args and the remaining arguments.
func flag(args []string, short, long string) (string, []string) {
	rest := make([]string, 0, len(args))
	value := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case (arg == short || arg == long) && i+1 < len(args):
			value = args[i+1]
			i++
		case strings.HasPrefix(arg, long+"="):
			value = strings.TrimPrefix(arg, long+"=")
		default:
			rest = append(rest, arg)
		}
	}
	return value, rest
}

// has reports whether a boolean flag is present, and the remaining arguments.
func has(args []string, names ...string) (bool, []string) {
	found := false
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		matched := false
		for _, name := range names {
			if arg == name {
				matched = true
			}
		}
		if matched {
			found = true
		} else {
			rest = append(rest, arg)
		}
	}
	return found, rest
}
