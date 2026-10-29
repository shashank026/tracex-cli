package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/gitrepo"
)

// The coding agent reads a file, edits it (approved), runs a command
// (declined), then answers; the fake API plays the model.
func TestTheCodingAgentRunsToolsLocallyWithApproval(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "signup.ts"), []byte("const name = input.trim()\n"), 0o644)

	var seen [][]api.CodeTurn
	replies := []string{
		`{"tool_calls":[{"id":"1","name":"read_file","input":{"path":"signup.ts"}}],"raw":[{"type":"thinking","signature":"s"}],"raw_provider":"anthropic"}`,
		`{"tool_calls":[{"id":"2","name":"edit_file","input":{"path":"signup.ts","old_str":"input.trim()","new_str":"input.trim() || 'there'"}}]}`,
		`{"tool_calls":[{"id":"3","name":"run_command","input":{"command":"rm -rf /tmp/whatever"}}]}`,
		`{"text":"Fixed the empty name in signup.ts:1."}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Turns   []api.CodeTurn `json:"turns"`
			Context string         `json:"context"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Context, root) {
			t.Errorf("context does not name the repository: %q", body.Context)
		}
		seen = append(seen, body.Turns)
		_, _ = io.WriteString(w, replies[len(seen)-1])
	}))
	defer server.Close()

	answers := []string{"y", "n"} // approve the edit, decline the command
	c := newCoding(api.New(server.URL, "tok", "test"), &gitrepo.Repo{Root: root}, func(string) (string, error) {
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	})
	c.interactive = true
	out := capture(t, func() {
		if err := c.ask(context.Background(), "the welcome message has no name"); err != nil {
			t.Fatal(err)
		}
	})

	data, _ := os.ReadFile(filepath.Join(root, "signup.ts"))
	if string(data) != "const name = input.trim() || 'there'\n" {
		t.Fatalf("edit not applied: %q", data)
	}
	if !strings.Contains(out, "Edit signup.ts") || !strings.Contains(out, "Fixed the empty name") {
		t.Fatalf("output:\n%s", out)
	}
	// What each tool returned went back to the model; thinking blocks unchanged
	last := seen[3]
	if last[1].RawProvider != "anthropic" || len(last[1].Raw) != 1 {
		t.Fatalf("raw blocks not sent back: %+v", last[1])
	}
	if got := last[2].Results[0].Content; !strings.Contains(got, "input.trim()") {
		t.Fatalf("read result: %q", got)
	}
	if got := last[6].Results[0]; !got.IsError || !strings.Contains(got.Content, "declined") {
		t.Fatalf("declined command result: %+v", got)
	}
}

func TestWithoutATerminalEditsAreRefused(t *testing.T) {
	c := &coding{always: map[string]bool{}, confirm: func(string) (string, error) { return "y", nil }}
	if c.allowed("edit", "Make this edit?") {
		t.Fatal("an edit must not be allowed without someone to approve it")
	}
}
