// Package config keeps the CLI's sign-in (~/.tracex/credentials.json) and a
// repository's link to its TraceX project (.tracex.json at the repository root).
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultAPIURL is TraceX's API (/v1); the dashboard is app.tracexpro.com.
const DefaultAPIURL = "https://api.tracexpro.com"

// ProjectFile links a repository to a TraceX project. It holds no secrets and
// can be committed so the whole team asks the same project.
const ProjectFile = ".tracex.json"

// Credentials is one signed-in session of the CLI.
type Credentials struct {
	APIURL       string    `json:"api_url"`
	AppURL       string    `json:"app_url,omitempty"` // the dashboard, for links
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Organization string    `json:"organization"`
	// The project `tracex -c` uses outside a linked repository
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
}

// Project is the content of .tracex.json.
type Project struct {
	ProjectID    string `json:"project_id"`
	ProjectName  string `json:"project_name"`
	Organization string `json:"organization,omitempty"`
	Repository   string `json:"repository"`
	Branch       string `json:"branch"`
}

// Dir is ~/.tracex (TRACEX_HOME overrides it).
func Dir() (string, error) {
	if dir := os.Getenv("TRACEX_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tracex"), nil
}

func credentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// ErrSignedOut means there is no usable sign-in.
var ErrSignedOut = errors.New("not signed in: run `tracex login`")

// LoadCredentials returns the saved sign-in, or ErrSignedOut.
func LoadCredentials() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrSignedOut
	}
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil || creds.AccessToken == "" {
		return nil, ErrSignedOut
	}
	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return nil, errors.New("your sign-in expired: run `tracex login`")
	}
	return &creds, nil
}

// SaveCredentials writes the sign-in, readable only by this user.
func SaveCredentials(creds *Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteCredentials signs the CLI out locally.
func DeleteCredentials() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// APIURL is TRACEX_API_URL, else the signed-in server, else the default.
func APIURL(creds *Credentials) string {
	if url := os.Getenv("TRACEX_API_URL"); url != "" {
		return strings.TrimRight(url, "/")
	}
	if creds != nil && creds.APIURL != "" {
		return strings.TrimRight(creds.APIURL, "/")
	}
	return DefaultAPIURL
}

// FindProject reads the nearest .tracex.json from dir upwards (nil if none).
func FindProject(dir string) (*Project, string, error) {
	for {
		path := filepath.Join(dir, ProjectFile)
		raw, err := os.ReadFile(path)
		if err == nil {
			var project Project
			if err := json.Unmarshal(raw, &project); err != nil {
				return nil, path, err
			}
			return &project, path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, "", nil
		}
		dir = parent
	}
}

// SaveProject writes .tracex.json into the repository root.
func SaveProject(root string, project *Project) (string, error) {
	raw, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, ProjectFile)
	return path, os.WriteFile(path, append(raw, '\n'), 0o644)
}
