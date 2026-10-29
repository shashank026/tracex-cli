package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/config"
	"github.com/shashank026/tracex-cli/internal/ui"
)

// login signs the CLI in through the browser: TraceX shows a code, the person
// approves it in the dashboard (with their usual SSO or MFA), and the CLI gets
// a session of its own. No password is ever typed here.
func login(ctx context.Context, build Build, args []string) error {
	server, args := flag(args, "", "--api")
	noBrowser, _ := has(args, "--no-browser")
	base := config.APIURL(nil)
	if server != "" {
		base = strings.TrimRight(server, "/")
	}
	client := api.New(base, "", build.Version)
	device, err := client.StartDevice(ctx, clientName())
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  Your code:  %s\n\n", ui.Bold(device.UserCode))
	fmt.Printf("  Open %s\n", ui.Brand(device.VerificationURL))
	fmt.Printf("  %s\n\n", ui.Dim("and approve the code if it matches the one above."))
	if !noBrowser && ui.Interactive() {
		if openBrowser(device.VerificationURL) == nil {
			fmt.Printf("  %s\n\n", ui.Dim("Opened your browser."))
		}
	}

	interval := time.Duration(max(device.Interval, 2)) * time.Second
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	spinner := ui.StartSpinner("Waiting for you to approve in the browser…")
	defer spinner.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		session, err := client.PollDevice(ctx, device.DeviceCode)
		var apiErr *api.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Status >= 500:
			continue // a hiccup on the server: keep waiting
		case err != nil:
			return err
		case session.Status == "pending" || session.AccessToken == "":
			continue
		}
		spinner.Stop()
		creds := &config.Credentials{
			APIURL:       base,
			AccessToken:  session.AccessToken,
			ExpiresAt:    time.Now().Add(time.Duration(session.ExpiresIn) * time.Second),
			Email:        session.User.Email,
			Name:         session.User.Name,
			Organization: session.User.OrganizationName,
		}
		if old, err := config.LoadCredentials(); err == nil && old.APIURL == base {
			creds.ProjectID, creds.ProjectName = old.ProjectID, old.ProjectName
		}
		if err := config.SaveCredentials(creds); err != nil {
			return fmt.Errorf("signed in, but could not save the sign-in: %w", err)
		}
		ui.Success("Signed in as %s %s", ui.Bold(creds.Email), ui.Dim("("+creds.Organization+")"))
		fmt.Println(ui.Dim("  Next: run `tracex init` in a repository, then `tracex -c` to ask a question."))
		return nil
	}
	return errors.New("the code expired before it was approved: run `tracex login` again")
}

func logout(ctx context.Context, build Build) error {
	client, creds, err := signedIn(build)
	if errors.Is(err, config.ErrSignedOut) {
		ui.Info("Not signed in.")
		return nil
	}
	if err == nil {
		// End the session on the server too, so the token stops working everywhere
		if err := client.Logout(ctx); err != nil && !errors.Is(err, api.ErrUnauthorized) {
			ui.Fail("Could not end the session on %s (%s); removing it from this computer.", config.APIURL(creds), err)
		}
	}
	if err := config.DeleteCredentials(); err != nil {
		return err
	}
	ui.Success("Signed out.")
	return nil
}

// clientName is shown on the approval page, e.g. "ada-mbp (macOS)".
func clientName() string {
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".local")
	system := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[runtime.GOOS]
	if system == "" {
		system = runtime.GOOS
	}
	if host == "" {
		return "TraceX CLI on " + system
	}
	return fmt.Sprintf("TraceX CLI on %s (%s)", host, system)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
