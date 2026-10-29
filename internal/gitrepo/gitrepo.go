// Package gitrepo reads what `tracex init` needs from the local git checkout.
package gitrepo

import (
	"errors"
	"os/exec"
	"strings"
)

// Repo is the checkout the CLI runs in.
type Repo struct {
	Root   string // top-level directory
	Owner  string // GitHub owner, e.g. "acme"
	Name   string // repository, e.g. "api"
	Branch string // checked-out branch ("" when detached)
}

// FullName is "owner/name".
func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

// ErrNotARepo means the directory is not inside a git checkout.
var ErrNotARepo = errors.New("not a git repository: run `tracex init` inside your project's checkout")

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Open reads the repository at dir: its root, GitHub owner/name and branch.
func Open(dir string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is not installed")
	}
	root, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}
	remote, err := git(root, "remote", "get-url", "origin")
	if err != nil || remote == "" {
		names, _ := git(root, "remote")
		first := strings.Fields(names)
		if len(first) == 0 {
			return nil, errors.New("this repository has no remote: push it to GitHub first")
		}
		if remote, err = git(root, "remote", "get-url", first[0]); err != nil {
			return nil, err
		}
	}
	owner, name, ok := ParseRemote(remote)
	if !ok {
		return nil, errors.New("could not read the GitHub repository from remote " + remote)
	}
	branch, _ := git(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	return &Repo{Root: root, Owner: owner, Name: name, Branch: branch}, nil
}

// ParseRemote reads owner and repository from any remote form:
// git@github.com:acme/api.git, https://github.com/acme/api,
// ssh://git@github.com/acme/api.git, or an SSH alias such as work:acme/api.git.
func ParseRemote(remote string) (owner, name string, ok bool) {
	path := strings.TrimSpace(remote)
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
		if j := strings.Index(path, "/"); j >= 0 {
			path = path[j+1:] // drop the host
		}
	} else if i := strings.Index(path, ":"); i >= 0 {
		path = path[i+1:] // scp form: host:owner/name
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}
