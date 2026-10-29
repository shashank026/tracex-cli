package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/coder"
	"github.com/shashank026/tracex-cli/internal/gitrepo"
	"github.com/shashank026/tracex-cli/internal/ui"
)

const (
	// Tool rounds for one request before TraceX stops and reports
	maxCodeSteps = 60
	// Older tool output is dropped from what is sent again, past this many turns
	keepFullTurns = 24
)

// coding is `tracex -c` in a repository that is not linked to TraceX: a coding
// agent like Claude Code. The model runs through TraceX's API; its tools run
// here, and every edit and command waits for the person's yes.
type coding struct {
	client  *api.Client
	repo    coder.Repo
	name    string // owner/name or the folder's name
	branch  string
	turns   []api.CodeTurn
	always  map[string]bool // "edit" / "run": approved for the rest of the session
	confirm func(question string) (string, error)
	// Approvals need someone at a terminal; without one, edits and commands are refused
	interactive bool
}

func newCoding(client *api.Client, repo *gitrepo.Repo, confirm func(string) (string, error)) *coding {
	name := repo.FullName()
	if repo.Owner == "" {
		name = filepath.Base(repo.Root)
	}
	return &coding{
		client:      client,
		repo:        coder.Repo{Root: repo.Root},
		name:        name,
		branch:      repo.Branch,
		always:      map[string]bool{},
		confirm:     confirm,
		interactive: ui.Interactive(),
	}
}

// context tells the model where it is working.
func (c *coding) context() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s at %s", c.name, c.repo.Root)
	if c.branch != "" {
		fmt.Fprintf(&b, " (branch %s)", c.branch)
	}
	fmt.Fprintf(&b, ". System: %s/%s.", runtime.GOOS, runtime.GOARCH)
	if top, err := c.repo.ListFiles(""); err == nil {
		lines := strings.Split(top, "\n")
		var first []string
		for _, line := range lines {
			if !strings.Contains(strings.TrimSuffix(line, "/"), "/") {
				first = append(first, line)
			}
		}
		if len(first) > 60 {
			first = first[:60]
		}
		fmt.Fprintf(&b, "\nTop-level files: %s", strings.Join(first, ", "))
	}
	if status, err := exec.Command("git", "-C", c.repo.Root, "status", "--short").Output(); err == nil {
		changed := strings.Split(strings.TrimSpace(string(status)), "\n")
		if len(changed) > 0 && changed[0] != "" {
			if len(changed) > 30 {
				changed = append(changed[:30], "…")
			}
			fmt.Fprintf(&b, "\nUncommitted changes:\n%s", strings.Join(changed, "\n"))
		}
	}
	return b.String()
}

func (c *coding) reset() { c.turns = nil }

// ask works on one request until the model answers without tools.
func (c *coding) ask(parent context.Context, request string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		select {
		case <-interrupts:
			cancel()
		case <-ctx.Done():
		}
	}()

	c.turns = append(c.turns, api.CodeTurn{Role: "user", Text: request})
	where := c.context()
	spinner := ui.StartSpinner("Thinking…")
	defer func() { spinner.Stop() }()

	for step := 0; step < maxCodeSteps; step++ {
		reply, err := c.client.CodeTurn(ctx, c.sendable(), where)
		if ctx.Err() != nil {
			spinner.Stop()
			fmt.Println(ui.Dim("  ⎿ Stopped."))
			return nil
		}
		if err != nil {
			return err
		}
		c.turns = append(c.turns, api.CodeTurn{
			Role: "assistant", Text: reply.Text, ToolCalls: reply.ToolCalls,
			Raw: reply.Raw, RawProvider: reply.RawProvider,
		})
		if len(reply.ToolCalls) == 0 {
			spinner.Stop()
			fmt.Println()
			fmt.Println(ui.Markdown(firstNonEmpty(reply.Text, "Done.")))
			return nil
		}
		var results []api.ToolResult
		for _, call := range reply.ToolCalls {
			// Edits and commands are shown for approval: no spinner over the question
			asks := call.Name == "edit_file" || call.Name == "run_command"
			if asks {
				spinner.Stop()
			} else {
				spinner.Set(codeHeadline(call) + "…")
			}
			content, failed := c.run(ctx, call)
			if asks {
				spinner = ui.StartSpinner("Thinking…")
			}
			results = append(results, api.ToolResult{CallID: call.ID, Content: content, IsError: failed})
		}
		c.turns = append(c.turns, api.CodeTurn{Role: "tool", Results: results})
		spinner.Set("Thinking…")
	}
	spinner.Stop()
	fmt.Println(ui.Yellow("  ! ") + fmt.Sprintf("Stopped after %d steps. Say \"continue\" to keep going.", maxCodeSteps))
	return nil
}

// sendable is the conversation with old tool output shortened, so long
// sessions stay small.
func (c *coding) sendable() []api.CodeTurn {
	out := make([]api.CodeTurn, len(c.turns))
	copy(out, c.turns)
	for i := 0; i < len(out)-keepFullTurns; i++ {
		if out[i].Role != "tool" {
			continue
		}
		results := make([]api.ToolResult, len(out[i].Results))
		for j, r := range out[i].Results {
			if len(r.Content) > 400 {
				r.Content = r.Content[:400] + "\n… (older output removed)"
			}
			results[j] = r
		}
		out[i].Results = results
	}
	return out
}

func codeHeadline(call api.ToolCall) string {
	switch call.Name {
	case "list_files":
		return "Looking at the files"
	case "search":
		return "Searching the code"
	case "read_file":
		return "Reading " + filepath.Base(str(call.Input, "path"))
	case "edit_file":
		return "Editing " + filepath.Base(str(call.Input, "path"))
	case "run_command":
		return "Running a command"
	}
	return "Working"
}

// run executes one tool call; edits and commands ask first.
func (c *coding) run(ctx context.Context, call api.ToolCall) (string, bool) {
	in := call.Input
	switch call.Name {
	case "list_files":
		return result(c.repo.ListFiles(str(in, "path")))
	case "search":
		return result(c.repo.Search(ctx, str(in, "pattern"), str(in, "path"), str(in, "glob")))
	case "read_file":
		return result(c.repo.ReadFile(str(in, "path"), num(in, "start_line"), num(in, "limit")))
	case "edit_file":
		edit, err := c.repo.PrepareEdit(str(in, "path"), str(in, "old_str"), str(in, "new_str"))
		if err != nil {
			return err.Error(), true
		}
		printEdit(edit)
		if !c.allowed("edit", "Make this edit?") {
			return "The person declined this edit. Do not retry it; ask what they want instead.", true
		}
		if err := edit.Apply(); err != nil {
			return "could not write the file: " + err.Error(), true
		}
		if edit.Create {
			return "Created " + edit.Path, false
		}
		return "Edited " + edit.Path, false
	case "run_command":
		command := strings.TrimSpace(str(in, "command"))
		if command == "" {
			return "no command given", true
		}
		fmt.Printf("%s %s\n  %s\n", ui.Brand("●"), ui.Bold("Run a command"), ui.Yellow("$ "+command))
		if !c.allowed("run", "Run it?") {
			return "The person declined this command. Do not retry it; ask or take another approach.", true
		}
		running := ui.StartSpinner("Running…")
		out := c.repo.Run(ctx, command)
		running.Stop()
		fmt.Println("  " + ui.Dim("⎿ "+strings.SplitN(out, "\n", 2)[0]))
		return out, !strings.HasPrefix(out, "exit code 0")
	}
	return "unknown tool " + call.Name, true
}

// allowed asks for approval: yes, always (for this session), or no.
func (c *coding) allowed(kind, question string) bool {
	if c.always[kind] {
		return true
	}
	if c.confirm == nil || !c.interactive {
		fmt.Println(ui.Dim("  ⎿ Not allowed without a terminal to approve it."))
		return false
	}
	answer, err := c.confirm(fmt.Sprintf("  %s %s ", question, ui.Dim("[y]es · [a]lways · [n]o")))
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	case "a", "always":
		c.always[kind] = true
		return true
	}
	fmt.Println(ui.Dim("  ⎿ Declined."))
	return false
}

// printEdit shows a proposed edit as a small diff.
func printEdit(edit *coder.Edit) {
	title := "Edit " + edit.Path
	if edit.Create {
		title = "Create " + edit.Path
	}
	fmt.Println(ui.Brand("●") + " " + ui.Bold(title))
	show := func(text, sign string, paint func(string) string) {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		for i, line := range lines {
			if i == 30 {
				fmt.Println(ui.Dim(fmt.Sprintf("    … %d more lines", len(lines)-30)))
				break
			}
			fmt.Println("  " + paint(sign+" "+line))
		}
	}
	if !edit.Create {
		show(edit.Old, "-", ui.Red)
	}
	show(edit.New, "+", ui.Green)
}

func result(out string, err error) (string, bool) {
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

func str(in map[string]any, key string) string {
	if v, ok := in[key].(string); ok {
		return v
	}
	return ""
}

func num(in map[string]any, key string) int {
	if v, ok := in[key].(float64); ok {
		return int(v)
	}
	return 0
}

// codingRepo is the repository to work in when this folder is not linked to a
// TraceX project, or nil (ask TraceX about the project instead).
func codingRepo(cwd string) *gitrepo.Repo {
	repo, err := gitrepo.Open(cwd)
	if err == nil {
		return repo
	}
	if errors.Is(err, gitrepo.ErrNotARepo) {
		return nil
	}
	// A git checkout without a GitHub remote is still a codebase
	if root, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		return &gitrepo.Repo{Root: strings.TrimSpace(string(root))}
	}
	return nil
}
