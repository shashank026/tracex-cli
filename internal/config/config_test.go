package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialsArePrivateAndExpire(t *testing.T) {
	t.Setenv("TRACEX_HOME", t.TempDir())
	if _, err := LoadCredentials(); err != ErrSignedOut {
		t.Fatalf("got %v, want ErrSignedOut", err)
	}
	creds := &Credentials{APIURL: "http://x", AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}
	if err := SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
	dir, _ := Dir()
	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file mode %v (%v), want 0600", info.Mode().Perm(), err)
	}
	if got, err := LoadCredentials(); err != nil || got.AccessToken != "t" {
		t.Fatalf("got %+v %v", got, err)
	}
	creds.ExpiresAt = time.Now().Add(-time.Minute)
	_ = SaveCredentials(creds)
	if _, err := LoadCredentials(); err == nil {
		t.Fatal("an expired sign-in should not load")
	}
}

func TestProjectFileIsFoundFromSubfolders(t *testing.T) {
	root := t.TempDir()
	if _, err := SaveProject(root, &Project{ProjectID: "p1", ProjectName: "Checkout"}); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "src", "billing")
	_ = os.MkdirAll(deep, 0o755)
	project, path, err := FindProject(deep)
	if err != nil || project == nil || project.ProjectID != "p1" || path != filepath.Join(root, ProjectFile) {
		t.Fatalf("got %+v %s %v", project, path, err)
	}
}
