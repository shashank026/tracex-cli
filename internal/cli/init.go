package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tracexpro/tracex-cli/internal/api"
	"github.com/tracexpro/tracex-cli/internal/config"
	"github.com/tracexpro/tracex-cli/internal/gitrepo"
	"github.com/tracexpro/tracex-cli/internal/ui"
)

// initRepo links the git repository in this folder to a TraceX project: it
// picks (or creates) the project, connects the repository through the TraceX
// GitHub App if it is not connected yet, and writes .tracex.json.
func initRepo(ctx context.Context, build Build, args []string) error {
	projectFlag, args := flag(args, "-p", "--project")
	branchFlag, args := flag(args, "-b", "--branch")
	yes, _ := has(args, "-y", "--yes")

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	repo, err := gitrepo.Open(cwd)
	if err != nil {
		return err
	}
	client, creds, err := signedIn(build)
	if err != nil {
		return err
	}
	ui.Info("Repository %s %s", ui.Bold(repo.FullName()), ui.Dim("("+repo.Root+")"))

	existing, _, _ := config.FindProject(repo.Root)
	project, err := pickProject(ctx, client, projectFlag, existing, yes)
	if err != nil {
		return err
	}

	connections, err := client.Connections(ctx, project.ID)
	if err != nil {
		return err
	}
	var connected *api.Connection
	for i, c := range connections {
		if strings.EqualFold(c.Owner+"/"+c.Repo, repo.FullName()) {
			connected = &connections[i]
			break
		}
	}
	if connected != nil {
		ui.Success("%s is connected to %s %s", repo.FullName(), ui.Bold(project.Name), ui.Dim("(branch "+connected.Branch+")"))
		if connected.Status == "failed" && connected.Problem != "" {
			fmt.Println(ui.Yellow("  ! ") + connected.Problem)
		}
	} else {
		connected, err = connect(ctx, client, project, repo, branchFlag, yes)
		if err != nil {
			return err
		}
	}

	path, err := config.SaveProject(repo.Root, &config.Project{
		ProjectID:    project.ID,
		ProjectName:  project.Name,
		Organization: creds.Organization,
		Repository:   repo.FullName(),
		Branch:       connected.Branch,
	})
	if err != nil {
		return err
	}
	ui.Success("Wrote %s %s", config.ProjectFile, ui.Dim("(no secrets: commit it so your team asks the same project)"))
	_ = path
	fmt.Println(ui.Dim("  Ask TraceX about this repository with `tracex -c`."))
	return nil
}

func pickProject(ctx context.Context, client *api.Client, wanted string, existing *config.Project, yes bool) (*api.Project, error) {
	projects, err := client.Projects(ctx)
	if err != nil {
		return nil, err
	}
	if wanted == "" && existing != nil {
		wanted = existing.ProjectID
	}
	for i, p := range projects {
		if wanted != "" && (p.ID == wanted || strings.EqualFold(p.Name, wanted)) {
			return &projects[i], nil
		}
	}
	if wanted != "" && existing == nil {
		return nil, fmt.Errorf("no project %q in your organization", wanted)
	}
	if len(projects) == 1 && (yes || !ui.Interactive()) {
		return &projects[0], nil
	}
	if !ui.Interactive() {
		if len(projects) == 0 {
			return nil, errors.New("your organization has no projects yet: create one in the dashboard or run tracex init in a terminal")
		}
		return nil, errors.New("several projects: pass --project <id or name>")
	}
	options := make([]string, 0, len(projects)+1)
	for _, p := range projects {
		options = append(options, p.Name)
	}
	options = append(options, ui.Dim("Create a new project"))
	choice := ui.Choose("Which TraceX project is this repository part of?", options, 0)
	if choice < len(projects) {
		return &projects[choice], nil
	}
	name := ui.Ask("Project name:", "")
	if name == "" {
		return nil, errors.New("a project needs a name")
	}
	return client.CreateProject(ctx, name)
}

// connect finds the repository among the GitHub App's installations and
// connects one branch of it; without access, it explains how to grant it.
func connect(ctx context.Context, client *api.Client, project *api.Project, repo *gitrepo.Repo, branch string, yes bool) (*api.Connection, error) {
	installations, err := client.Installations(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	var found *api.Repository
	var installation api.Installation
	for _, inst := range installations {
		repos, err := client.Repositories(ctx, inst.ID)
		if err != nil {
			continue // an installation that was removed on GitHub
		}
		for i, r := range repos {
			if strings.EqualFold(r.FullName, repo.FullName()) {
				found, installation = &repos[i], inst
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		url, err := client.InstallURL(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		ui.Fail("TraceX's GitHub App cannot see %s yet.", repo.FullName())
		fmt.Printf("  Install it on %s and give it access to %s:\n  %s\n", ui.Bold(repo.Owner), ui.Bold(repo.Name), ui.Brand(url))
		fmt.Println(ui.Dim("  Then run `tracex init` again."))
		if ui.Interactive() {
			_ = openBrowser(url)
		}
		return nil, errQuiet
	}

	if branch == "" {
		branch = found.DefaultBranch
		if ui.Interactive() && !yes {
			fmt.Println(ui.Dim("  TraceX reads this branch when it answers questions and investigates bugs."))
			branch = ui.Ask("Branch to connect:", found.DefaultBranch)
		}
	}
	branches, err := client.Branches(ctx, installation.ID, found.Owner, found.Name)
	if err == nil {
		ok := false
		for _, b := range branches {
			ok = ok || b.Name == branch
		}
		if !ok {
			return nil, fmt.Errorf("branch %q is not on GitHub: push it first or pick another with --branch", branch)
		}
	}
	connection, err := client.Connect(ctx, api.ConnectRequest{
		ProjectID:      project.ID,
		InstallationID: installation.ID,
		RepositoryID:   found.ID,
		Owner:          found.Owner,
		Repository:     found.Name,
		Branch:         branch,
	})
	if err != nil {
		return nil, err
	}
	ui.Success("Connected %s (%s) to %s", repo.FullName(), branch, ui.Bold(project.Name))
	return connection, nil
}
