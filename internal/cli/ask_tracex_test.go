package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/coder"
)

func TestAskTraceXOnlyInALinkedRepository(t *testing.T) {
	c := &coding{repo: coder.Repo{Root: t.TempDir()}, always: map[string]bool{}}
	if c.projectForTools() != "" {
		t.Fatal("an unlinked repository must not offer ask_tracex")
	}
	content, failed := c.run(context.Background(), api.ToolCall{Name: "ask_tracex", Input: map[string]any{"question": "how many signups?"}})
	if !failed || !strings.Contains(content, "linked") {
		t.Fatalf("unlinked ask_tracex should fail plainly, got %q", content)
	}

	var asked string
	c.project = "Payments"
	c.askTraceX = func(_ context.Context, q string, status func(string)) (string, error) {
		asked = q
		status("Checking the database")
		return "42 signups yesterday [S1]", nil
	}
	if c.projectForTools() != "Payments" {
		t.Fatal("a linked repository should offer ask_tracex")
	}
	content, failed = c.run(context.Background(), api.ToolCall{Name: "ask_tracex", Input: map[string]any{"question": "how many signups?"}})
	if failed || asked != "how many signups?" || !strings.Contains(content, "42 signups") {
		t.Fatalf("got %q (failed=%v, asked %q)", content, failed, asked)
	}

	c.askTraceX = func(context.Context, string, func(string)) (string, error) {
		return "", errors.New("plan limit reached")
	}
	content, failed = c.run(context.Background(), api.ToolCall{Name: "ask_tracex", Input: map[string]any{"question": "x"}})
	if !failed || !strings.Contains(content, "plan limit reached") {
		t.Fatalf("errors should reach the model, got %q", content)
	}
}
