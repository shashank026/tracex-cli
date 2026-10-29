package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shashank026/tracex-cli/internal/config"
)

func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "feature"}, {"remote", "add", "origin", remote}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestInitConnectsTheRepositoryAndWritesTheProjectFile(t *testing.T) {
	var connected map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/projects":
			_, _ = io.WriteString(w, `[{"id":"p1","name":"Checkout"},{"id":"p2","name":"Other"}]`)
		case "GET /v1/codebase-connections":
			_, _ = io.WriteString(w, `[]`)
		case "GET /v1/codebase-connections/github/installations":
			_, _ = io.WriteString(w, `[{"installation_id":7,"account_login":"acme"}]`)
		case "GET /v1/codebase-connections/github/installations/7/repositories":
			_, _ = io.WriteString(w, `[{"id":99,"owner":"acme","name":"api","full_name":"acme/api","default_branch":"main"}]`)
		case "GET /v1/codebase-connections/github/installations/7/repositories/acme/api/branches":
			_, _ = io.WriteString(w, `[{"name":"main"}]`)
		case "POST /v1/codebase-connections":
			_ = json.NewDecoder(r.Body).Decode(&connected)
			_, _ = io.WriteString(w, `{"connection_id":"c1","owner":"acme","repository":"api","branch":"main"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("TRACEX_HOME", t.TempDir())
	t.Setenv("TRACEX_API_URL", server.URL)
	_ = config.SaveCredentials(&config.Credentials{APIURL: server.URL, AccessToken: "tok", Organization: "Acme"})
	repo := gitRepo(t, "git@github.com:acme/api.git")
	t.Chdir(repo)

	if err := initRepo(context.Background(), Build{Version: "test"}, []string{"--project", "Checkout"}); err != nil {
		t.Fatal(err)
	}
	if connected["installation_id"] != float64(7) || connected["repository_id"] != float64(99) || connected["branch"] != "main" {
		t.Fatalf("connected with %v", connected)
	}
	raw, err := os.ReadFile(filepath.Join(repo, config.ProjectFile))
	if err != nil {
		t.Fatal(err)
	}
	var project config.Project
	_ = json.Unmarshal(raw, &project)
	if project.ProjectID != "p1" || project.Repository != "acme/api" || project.Branch != "main" {
		t.Fatalf("wrote %s", raw)
	}
}
