package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chzyer/readline"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/config"
	"github.com/shashank026/tracex-cli/internal/ui"
)

// What each step is called, as in the dashboard's conversation view
var stepNames = map[string]string{
	"plan": "Plan", "list_sources": "Sources", "list_tables": "Tables", "describe_table": "Describe",
	"query": "Query", "count": "Count", "search_code": "Search", "read_file": "Read",
	"list_files": "Files", "recent_commits": "Commits", "aggregate": "Aggregate",
	"related_tables": "Links", "find_fields": "Fields", "export_data": "Export",
	"create_document": "Document", "verify": "Check", "find_records": "Find",
	"entity_timeline": "Timeline", "compare_periods": "Compare", "column_values": "Values",
	"similar_investigations": "History", "change_history": "Changes", "deploys": "Deploys",
	"read_pull_request": "PullRequest", "search_logs": "Logs", "search_errors": "Errors",
	"error_details": "StackTrace",
}

var thinkingWords = []string{"Thinking", "Reasoning", "Connecting the dots", "Cross-checking", "Pondering"}

type session struct {
	client   *api.Client
	creds    *config.Credentials
	project  api.Project
	threadID string
}

// chat answers one question (`tracex -c "..."`, or piped input) or opens an
// interactive session like the dashboard's Conversations.
func chat(ctx context.Context, build Build, args []string) error {
	projectFlag, args := flag(args, "-p", "--project")
	client, creds, err := signedIn(build)
	if err != nil {
		return err
	}
	project, err := chatProject(ctx, client, creds, projectFlag)
	if err != nil {
		return err
	}
	s := &session{client: client, creds: creds, project: *project}

	question := strings.TrimSpace(strings.Join(args, " "))
	if question == "" && !ui.Interactive() {
		piped, _ := io.ReadAll(os.Stdin)
		question = strings.TrimSpace(string(piped))
	}
	if question != "" {
		return s.ask(ctx, question)
	}
	return s.repl(ctx)
}

// chatProject: --project, else the repository's .tracex.json, else the saved
// default, else the only project, else ask (and remember the answer).
func chatProject(ctx context.Context, client *api.Client, creds *config.Credentials, wanted string) (*api.Project, error) {
	if wanted == "" {
		cwd, _ := os.Getwd()
		if linked, _, _ := config.FindProject(cwd); linked != nil {
			return &api.Project{ID: linked.ProjectID, Name: linked.ProjectName}, nil
		}
		if creds.ProjectID != "" {
			return &api.Project{ID: creds.ProjectID, Name: creds.ProjectName}, nil
		}
	}
	projects, err := client.Projects(ctx)
	if err != nil {
		return nil, err
	}
	var picked *api.Project
	for i, p := range projects {
		if wanted != "" && (p.ID == wanted || strings.EqualFold(p.Name, wanted)) {
			picked = &projects[i]
		}
	}
	switch {
	case wanted != "" && picked == nil:
		return nil, fmt.Errorf("no project %q in your organization", wanted)
	case picked != nil:
	case len(projects) == 0:
		return nil, errors.New("your organization has no projects yet: create one in the dashboard")
	case len(projects) == 1:
		picked = &projects[0]
	case !ui.Interactive():
		return nil, errors.New("several projects: pass --project <id or name>, or run tracex init in a repository")
	default:
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.Name
		}
		picked = &projects[ui.Choose("Which project do you want to ask about?", names, 0)]
	}
	if wanted == "" {
		creds.ProjectID, creds.ProjectName = picked.ID, picked.Name
		_ = config.SaveCredentials(creds)
	}
	return picked, nil
}

func (s *session) banner() {
	width := min(ui.Width()-2, 64)
	line := strings.Repeat("─", width-2)
	row := func(text, styled string) {
		pad := width - 4 - len([]rune(text))
		fmt.Printf("%s %s%s %s\n", ui.Brand("│"), styled, strings.Repeat(" ", max(pad, 0)), ui.Brand("│"))
	}
	fmt.Println(ui.Brand("╭" + line + "╮"))
	row("✻ TraceX", ui.Brand("✻")+" "+ui.Bold("TraceX"))
	row("", "")
	row("  Ask about your data, code and recent changes.", ui.Dim("  Ask about your data, code and recent changes."))
	where := fmt.Sprintf("  %s · %s", s.creds.Organization, s.project.Name)
	row(where, ui.Dim(where))
	fmt.Println(ui.Brand("╰" + line + "╯"))
	fmt.Println(ui.Dim("  /help for commands · ctrl+c stops an answer · ctrl+d exits"))
	fmt.Println()
}

func (s *session) repl(ctx context.Context) error {
	s.banner()
	historyFile := ""
	if dir, err := config.Dir(); err == nil {
		_ = os.MkdirAll(dir, 0o700)
		historyFile = filepath.Join(dir, "history")
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:            ui.Brand("> "),
		HistoryFile:       historyFile,
		HistorySearchFold: true,
		InterruptPrompt:   "^C",
		EOFPrompt:         "",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if errors.Is(err, readline.ErrInterrupt) {
			if line == "" {
				fmt.Println(ui.Dim("  (ctrl+d or /exit to quit)"))
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// A line ending in \ continues on the next one
		for strings.HasSuffix(line, "\\") {
			rl.SetPrompt(ui.Dim("… "))
			more, err := rl.Readline()
			rl.SetPrompt(ui.Brand("> "))
			if err != nil {
				break
			}
			line = strings.TrimSuffix(line, "\\") + "\n" + more
		}
		question := strings.TrimSpace(line)
		if question == "" {
			continue
		}
		if strings.HasPrefix(question, "/") {
			if s.command(ctx, question) {
				return nil
			}
			continue
		}
		if err := s.ask(ctx, question); err != nil {
			if errors.Is(err, api.ErrUnauthorized) {
				return err
			}
			ui.Fail("%s", err)
		}
		fmt.Println()
	}
}

// command runs a /command; true means quit.
func (s *session) command(ctx context.Context, line string) bool {
	name, arg, _ := strings.Cut(line, " ")
	switch name {
	case "/exit", "/quit", "/q":
		return true
	case "/new", "/clear":
		s.threadID = ""
		if name == "/clear" {
			fmt.Print("\x1b[H\x1b[2J")
			s.banner()
		}
		ui.Info("New conversation.")
	case "/project":
		project, err := chatProject(ctx, s.client, s.creds, strings.TrimSpace(arg))
		if strings.TrimSpace(arg) == "" {
			s.creds.ProjectID = ""
			project, err = chatProject(ctx, s.client, s.creds, "")
		}
		if err != nil {
			ui.Fail("%s", err)
			return false
		}
		s.project, s.threadID = *project, ""
		ui.Info("Asking %s now.", ui.Bold(project.Name))
	case "/open":
		url := strings.TrimRight(s.client.BaseURL, "/") + "/conversations"
		if s.threadID != "" {
			url += "/" + s.threadID
		}
		if openBrowser(url) != nil {
			fmt.Println(url)
		}
	case "/help", "/?":
		fmt.Println(`  /new        Start a new conversation
  /clear      Clear the screen and start a new conversation
  /project    Switch project (/project <name> to pick one)
  /open       Open this conversation in the dashboard
  /exit       Quit (or ctrl+d)

  End a line with \ to continue the question on the next line.`)
	default:
		ui.Fail("Unknown command %s: try /help.", name)
	}
	return false
}

// ask streams one answer: the steps as they happen, then the answer.
func (s *session) ask(parent context.Context, question string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	var messageID, threadID atomic.Value // set by the stream, read on ctrl+c
	var stopped atomic.Bool
	go func() {
		select {
		case <-interrupts:
			stopped.Store(true)
			thread, _ := threadID.Load().(string)
			message, _ := messageID.Load().(string)
			if message != "" {
				// Stop the answer on the server too, not just here
				stopCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
				_ = s.client.Stop(stopCtx, thread, message)
				done()
			}
			cancel()
		case <-ctx.Done():
		}
	}()

	plain := !ui.Interactive()
	spinner := ui.StartSpinner(thinkingWords[0] + "…")
	defer spinner.Stop()
	var answer strings.Builder
	var final *api.Message
	steps := 0

	err := s.client.Ask(ctx, s.project.ID, s.threadID, question, func(event api.Event) error {
		switch event.Name {
		case "message":
			var opening api.Opening
			if json.Unmarshal(event.Data, &opening) == nil {
				s.threadID = opening.Thread.ID
				threadID.Store(opening.Thread.ID)
				messageID.Store(opening.Assistant.ID)
			}
		case "thinking":
			spinner.Set(thinkingWords[steps%len(thinkingWords)] + "…")
		case "reasoning":
			var r struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(event.Data, &r) == nil && strings.TrimSpace(r.Text) != "" {
				spinner.Set(ui.Dim(oneLine(r.Text, ui.Width()-30)))
			}
		case "step":
			var step api.Step
			if json.Unmarshal(event.Data, &step) != nil {
				return nil
			}
			if step.Phase == "step_started" {
				spinner.Set(oneLine(step.Status, ui.Width()-30) + "…")
				return nil
			}
			if step.Phase == "step_finished" {
				steps++
				if line := formatStep(step); !plain && line != "" {
					spinner.Println(line)
				}
			}
		case "delta":
			var d struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(event.Data, &d) == nil {
				answer.WriteString(d.Text)
				spinner.Set("Writing the answer…")
			}
		case "reset":
			answer.Reset()
		case "done", "error", "snapshot":
			var end struct {
				Detail  string      `json:"detail"`
				Message api.Message `json:"message"`
			}
			if json.Unmarshal(event.Data, &end) == nil {
				final = &end.Message
				if event.Name == "error" {
					final.Status, final.Error = "error", firstNonEmpty(end.Detail, end.Message.Error)
				}
			}
		}
		return nil
	})
	spinner.Stop()

	if stopped.Load() {
		if text := strings.TrimSpace(answer.String()); text != "" {
			fmt.Println(ui.Markdown(text))
		}
		fmt.Println(ui.Dim("  ⎿ Stopped."))
		return nil
	}
	if err != nil {
		return err
	}
	if final != nil && final.Status == "error" {
		return errors.New(firstNonEmpty(final.Error, "TraceX could not answer"))
	}
	text := answer.String()
	if final != nil && final.Content != "" {
		text = final.Content
	}
	text = s.absoluteLinks(text)
	if plain {
		fmt.Println(text)
		return nil
	}
	fmt.Println()
	fmt.Println(ui.Markdown(text))
	if final != nil && len(final.Sources) > 0 {
		fmt.Println(formatSources(final.Sources))
	}
	return nil
}

// absoluteLinks makes the answer's dashboard links (files, investigations) clickable.
func (s *session) absoluteLinks(text string) string {
	base := strings.TrimRight(s.client.BaseURL, "/")
	return strings.ReplaceAll(text, "](/", "]("+base+"/")
}

// formatStep prints a finished step like a coding agent's trace: what it said
// it would do as a plain line, then one quiet line for the action itself.
func formatStep(step api.Step) string {
	var lines []string
	switch step.Tool {
	case "plan", "think":
		if text := firstNonEmpty(step.Thought, step.Status); text != "" {
			return wrap(text)
		}
		return ""
	}
	if step.Thought != "" {
		lines = append(lines, wrap(step.Thought))
	}
	label := firstNonEmpty(step.Done, step.Status, stepNames[step.Tool], step.Tool)
	verb, object, _ := strings.Cut(label, " ")
	if step.Tool == "read_file" {
		object = filepath.Base(object)
	}
	dot := ui.Dim("•")
	if step.OK != nil && !*step.OK {
		dot = ui.Yellow("•")
	}
	lines = append(lines, "  "+dot+" "+verb+" "+ui.Dim(oneLine(object, ui.Width()-len(verb)-6)))
	return strings.Join(lines, "\n")
}

func wrap(text string) string {
	return oneLine(text, 4*ui.Width())
}

func formatSources(sources []map[string]any) string {
	var parts []string
	for _, source := range sources {
		id, _ := source["id"].(string)
		where, _ := source["reference"].(string)
		if where == "" {
			where, _ = source["summary"].(string)
		}
		if id != "" && where != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", id, oneLine(where, 60)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ui.Dim("  Sources: " + strings.Join(parts, " · "))
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if limit > 1 && len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
