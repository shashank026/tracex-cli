package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadEvents(t *testing.T) {
	body := "event: message\ndata: {\"a\":1}\n\nevent: delta\ndata: {\"text\":\"hi\"}\n\n: keepalive\n\nevent: done\ndata: {}\n\n"
	var names []string
	if err := ReadEvents(strings.NewReader(body), func(e Event) error {
		names = append(names, e.Name+"="+string(e.Data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := `message={"a":1},delta={"text":"hi"},done={}`
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestErrorsUseTraceXsExplanation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.UserAgent(), "TraceX-CLI/1.2.0 (") {
			t.Errorf("user agent %q", r.UserAgent())
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"This branch is already connected."}`))
	}))
	defer server.Close()
	err := New(server.URL, "", "v1.2.0").JSON(context.Background(), "GET", "/v1/x", nil, nil)
	if err == nil || err.Error() != "This branch is already connected." {
		t.Fatalf("got %v", err)
	}
	err = New(server.URL, "tok", "v1").JSON(context.Background(), "GET", "/v1/x", nil, nil)
	if err != ErrUnauthorized {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
}
