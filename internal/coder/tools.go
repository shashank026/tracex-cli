// Package coder runs the coding agent's tools on this machine: list, search
// and read files freely, and edit files or run commands only when the person
// approves (https://ghuntley.com/agent/: a loop of model turns and tools).
package coder

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxListEntries  = 400
	maxMatches      = 200
	maxLineChars    = 300
	defaultReadLine = 400
	maxReadLines    = 2000
	maxOutputChars  = 30_000
	commandTimeout  = 2 * time.Minute
)

// Folders nobody wants listed or searched
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	".next": true, "__pycache__": true, ".venv": true, "venv": true, "target": true,
	".turbo": true, ".cache": true, "coverage": true,
}

// Repo is the folder the agent works in; no path leaves it.
type Repo struct{ Root string }

// ErrOutside is returned for a path outside the repository.
var ErrOutside = errors.New("that path is outside the repository")

// Path resolves a path the model gave, inside the repository.
func (r Repo) Path(rel string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimSpace(rel))
	full := filepath.Join(r.Root, clean)
	root, _ := filepath.EvalSymlinks(r.Root)
	if root == "" {
		root = r.Root
	}
	// Follow symlinks of what exists, so a link cannot point outside
	check := full
	for {
		if resolved, err := filepath.EvalSymlinks(check); err == nil {
			rest, _ := filepath.Rel(check, full)
			resolved = filepath.Join(resolved, rest)
			if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
				return "", ErrOutside
			}
			return full, nil
		}
		parent := filepath.Dir(check)
		if parent == check {
			return full, nil
		}
		check = parent
	}
}

func (r Repo) rel(full string) string {
	rel, err := filepath.Rel(r.Root, full)
	if err != nil {
		return full
	}
	return filepath.ToSlash(rel)
}

// ListFiles lists two levels under path; folders end with /.
func (r Repo) ListFiles(path string) (string, error) {
	base, err := r.Path(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(base)
	if err != nil {
		return "", fmt.Errorf("no folder %q", path)
	}
	if !info.IsDir() {
		return r.rel(base), nil
	}
	var entries []string
	walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == base {
			return nil
		}
		under, _ := filepath.Rel(base, p)
		depth := strings.Count(filepath.ToSlash(under), "/")
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			entries = append(entries, r.rel(p)+"/")
			if depth >= 1 {
				return fs.SkipDir
			}
			return nil
		}
		entries = append(entries, r.rel(p))
		if len(entries) >= maxListEntries {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return "(empty folder)", nil
	}
	return strings.Join(entries, "\n"), nil
}

// Search runs ripgrep (or a plain scan without it) for pattern.
func (r Repo) Search(ctx context.Context, pattern, path, glob string) (string, error) {
	base, err := r.Path(path)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("rg"); err == nil {
		args := []string{"--line-number", "--with-filename", "--no-heading", "--color=never",
			"--max-columns", fmt.Sprint(maxLineChars), "--max-columns-preview", "--smart-case"}
		for dir := range skipDirs {
			args = append(args, "--glob", "!"+dir+"/")
		}
		if glob != "" {
			args = append(args, "--glob", glob)
		}
		args = append(args, "--", pattern, base)
		cmd := exec.CommandContext(ctx, "rg", args...)
		out, err := cmd.Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "(no matches)", nil
		}
		if err != nil {
			return "", fmt.Errorf("search failed: %v", err)
		}
		return r.trimMatches(strings.Split(strings.TrimSpace(string(out)), "\n")), nil
	}
	return r.scan(pattern, base, glob)
}

func (r Repo) trimMatches(lines []string) string {
	prefix := r.Root + string(os.PathSeparator)
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, prefix)
	}
	if len(lines) > maxMatches {
		return strings.Join(lines[:maxMatches], "\n") + fmt.Sprintf("\n… %d more matches: narrow the search", len(lines)-maxMatches)
	}
	return strings.Join(lines, "\n")
}

func (r Repo) scan(pattern, base, glob string) (string, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %v", err)
	}
	var lines []string
	_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		data, err := os.ReadFile(p)
		if err != nil || len(data) > 2<<20 || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 1; scanner.Scan(); n++ {
			if re.MatchString(scanner.Text()) {
				line := scanner.Text()
				if len(line) > maxLineChars {
					line = line[:maxLineChars]
				}
				lines = append(lines, fmt.Sprintf("%s:%d:%s", r.rel(p), n, line))
			}
		}
		return nil
	})
	if len(lines) == 0 {
		return "(no matches)", nil
	}
	return r.trimMatches(lines), nil
}

// ReadFile returns numbered lines from start (1-based).
func (r Repo) ReadFile(path string, start, limit int) (string, error) {
	full, err := r.Path(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("no file %q", path)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s is a binary file", path)
	}
	lines := strings.Split(string(data), "\n")
	start = max(start, 1)
	if limit <= 0 {
		limit = defaultReadLine
	}
	limit = min(limit, maxReadLines)
	end := min(start-1+limit, len(lines))
	if start > len(lines) {
		return fmt.Sprintf("(%s has %d lines)", path, len(lines)), nil
	}
	var b strings.Builder
	for i := start - 1; i < end; i++ {
		line := lines[i]
		if len(line) > 2*maxLineChars {
			line = line[:2*maxLineChars] + "…"
		}
		fmt.Fprintf(&b, "%5d  %s\n", i+1, line)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "… %d more lines (start_line=%d to continue)", len(lines)-end, end+1)
	}
	return b.String(), nil
}

// Edit is a proposed change to one file, checked before it is shown.
type Edit struct {
	Path    string
	Full    string
	Old     string
	New     string
	Create  bool
	content string
}

// PrepareEdit checks an edit can be applied (old_str found exactly once).
func (r Repo) PrepareEdit(path, oldStr, newStr string) (*Edit, error) {
	full, err := r.Path(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	switch {
	case errors.Is(err, fs.ErrNotExist) && oldStr == "":
		return &Edit{Path: path, Full: full, New: newStr, Create: true}, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("no file %q (use an empty old_str to create it)", path)
	case err != nil:
		return nil, err
	case oldStr == "":
		return nil, fmt.Errorf("%s already exists: give old_str to change it", path)
	}
	content := string(data)
	switch n := strings.Count(content, oldStr); n {
	case 0:
		return nil, fmt.Errorf("old_str was not found in %s: read the file again and copy it exactly", path)
	case 1:
	default:
		return nil, fmt.Errorf("old_str appears %d times in %s: include more lines so it is unique", n, path)
	}
	return &Edit{Path: path, Full: full, Old: oldStr, New: newStr, content: content}, nil
}

// Apply writes the edit.
func (e *Edit) Apply() error {
	if e.Create {
		if err := os.MkdirAll(filepath.Dir(e.Full), 0o755); err != nil {
			return err
		}
		return os.WriteFile(e.Full, []byte(e.New), 0o644)
	}
	info, err := os.Stat(e.Full)
	if err != nil {
		return err
	}
	return os.WriteFile(e.Full, []byte(strings.Replace(e.content, e.Old, e.New, 1)), info.Mode().Perm())
}

// Run runs a shell command in the repository root, with a time limit.
func (r Repo) Run(ctx context.Context, command string) string {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.Root
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("stopped after %s (time limit)\n%s", commandTimeout, clip(out.String()))
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return "could not run the command: " + err.Error()
	}
	return fmt.Sprintf("exit code %d\n%s", code, clip(out.String()))
}

// clip keeps the start and end of long output, where the useful parts are.
func clip(s string) string {
	if len(s) <= maxOutputChars {
		return s
	}
	half := maxOutputChars / 2
	return s[:half] + fmt.Sprintf("\n… %d characters cut …\n", len(s)-maxOutputChars) + s[len(s)-half:]
}
