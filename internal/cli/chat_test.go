package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tracexpro/tracex-cli/internal/api"
	"github.com/tracexpro/tracex-cli/internal/config"
)

// A TraceX server that answers one question the way /v1/chats streams it.
func fakeTraceX(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/projects":
			_, _ = io.WriteString(w, `[{"id":"p1","name":"Checkout"}]`)
		case "/v1/chats":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, strings.Join([]string{
				`event: message` + "\n" + `data: {"thread":{"id":"t1"},"assistant":{"id":"m1"}}`,
				`event: step` + "\n" + `data: {"phase":"step_finished","tool":"count","status":"Counting users","result":"count = 5","ok":true}`,
				`event: delta` + "\n" + `data: {"text":"draft"}`,
				`event: reset` + "\n" + `data: {}`,
				`event: delta` + "\n" + `data: {"text":"There are **5** users [S1]."}`,
				`event: done` + "\n" + `data: {"message":{"id":"m1","status":"complete","content":"There are **5** users [S1]. See [the file](/v1/files/f1)."}}`,
			}, "\n\n")+"\n\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestOneQuestionPrintsTheFinalAnswer(t *testing.T) {
	server := fakeTraceX(t)
	defer server.Close()
	t.Setenv("TRACEX_HOME", t.TempDir())
	t.Setenv("TRACEX_API_URL", server.URL)
	_ = config.SaveCredentials(&config.Credentials{APIURL: server.URL, AccessToken: "tok", Organization: "Acme"})
	t.Chdir(t.TempDir())

	out := capture(t, func() {
		if err := chat(context.Background(), Build{Version: "test"}, []string{"how", "many", "users?"}); err != nil {
			t.Fatal(err)
		}
	})
	want := "There are **5** users [S1]. See [the file](" + server.URL + "/v1/files/f1).\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
	// The only project was remembered for next time
	creds, _ := config.LoadCredentials()
	if creds.ProjectID != "p1" {
		t.Fatalf("project not remembered: %+v", creds)
	}
}

func TestStepsAreLabelledLikeTheDashboard(t *testing.T) {
	ok := false
	line := formatStep(api.Step{Tool: "read_file", Status: "Reading src/auth/auth.service.ts", Done: "Read src/auth/auth.service.ts", OK: &ok})
	if !strings.Contains(line, "Read auth.service.ts") {
		t.Fatalf("got %q", line)
	}
	said := formatStep(api.Step{Tool: "plan", Thought: "I'll trace the signup flow."})
	if said != "I'll trace the signup flow." {
		t.Fatalf("got %q", said)
	}
}

func capture(t *testing.T, run func()) string {
	t.Helper()
	read, write, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = write
	run()
	os.Stdout = stdout
	_ = write.Close()
	out, _ := io.ReadAll(read)
	return string(out)
}
