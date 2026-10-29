package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shashank026/tracex-cli/internal/api"
	"github.com/shashank026/tracex-cli/internal/config"
	"github.com/shashank026/tracex-cli/internal/ui"
)

// Trace, the TraceX mascot: the logo's 3×3 grid of cubes standing on two
// feet, its lighter core for a face. Half blocks make two pixels per line.
var mascot = []string{
	"██ ██ ██",
	"▄▄ ▄▄ ▄▄",
	"▀▀ ▀▀ ▀▀",
	"██ ██ ██",
	" ▀    ▀ ",
}

// paintMascot colors the cubes in the brand blue and the core light.
func paintMascot(line string, row int) string {
	if !ui.Color {
		return line
	}
	if row == 1 || row == 2 {
		cube := string([]rune(line)[:2])
		return ui.Brand(cube+" ") + "\x1b[38;5;153m" + cube + "\x1b[0m" + ui.Brand(" "+cube)
	}
	return ui.Brand(line)
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// width is how many columns s takes on screen (escape codes take none).
func width(s string) int { return utf8.RuneCountInString(ansi.ReplaceAllString(s, "")) }

func pad(s string, n int) string { return s + strings.Repeat(" ", max(n-width(s), 0)) }

func center(s string, n int) string {
	gap := max(n-width(s), 0)
	return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
}

// cutLeft keeps the end of a path, where the folder name is.
func cutLeft(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return "…" + string(runes[len(runes)-n+1:])
}

func cut(s string, n int) string {
	if width(s) <= n {
		return s
	}
	plain := []rune(ansi.ReplaceAllString(s, ""))
	return string(plain[:max(n-1, 0)]) + "…"
}

// welcome is the start screen: who and where on the left with Trace, tips and
// recent conversations on the right, in one box like a coding agent's.
func (s *session) welcome(ctx context.Context, build Build) {
	total := min(ui.Width()-2, 110)
	if total < 60 {
		s.compactWelcome(build)
		return
	}
	leftW := 36
	rightW := total - leftW - 7 // borders, padding and the divider

	first := strings.Fields(s.creds.Name)
	name := "back"
	if len(first) > 0 {
		name = "back " + first[0]
	}
	cwd, _ := os.Getwd()
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}

	usage := ""
	plan := ""
	if billing, err := s.client.Billing(ctx); err == nil {
		plan = billing.PlanName() + " plan"
		if left := tightest(billing); left >= 0 {
			usage = fmt.Sprintf("%d%% left", left)
		}
	}
	left := []string{
		"",
		ui.Bold(fmt.Sprintf("Welcome %s!", name)),
		"",
	}
	for i, line := range mascot {
		left = append(left, paintMascot(line, i))
	}
	left = append(left, "",
		ui.Dim(cut(strings.Join(nonEmpty(plan, usage), " · "), leftW)),
		ui.Dim(cut(s.creds.Organization+" · "+s.project.Name, leftW)),
		ui.Dim(cutLeft(cwd, leftW)),
	)

	right := []string{ui.Brand(ui.Bold("Tips for getting started"))}
	if linked, _, _ := config.FindProject(mustCwd()); linked == nil {
		right = append(right, cut("Run tracex init in a repository to link it to its project", rightW))
	} else {
		right = append(right, cut("Ask about your data, code and recent changes", rightW))
	}
	right = append(right,
		ui.Dim(cut(`Try "why did signups drop yesterday?"`, rightW)),
		ui.Dim(strings.Repeat("─", rightW)),
		ui.Brand(ui.Bold("Recent activity")),
	)
	threads, _ := s.client.Threads(ctx, s.project.ID)
	if len(threads) == 0 {
		right = append(right, ui.Dim("No recent activity"))
	}
	for _, t := range threads[:min(len(threads), 4)] {
		when := ago(t.UpdatedAt)
		right = append(right, cut(t.Title, rightW-len(when)-1)+" "+ui.Dim(when))
	}

	rows := max(len(left), len(right))
	title := " TraceX CLI " + build.Version + " "
	top := "╭─" + ui.Brand(title) + strings.Repeat("─", max(total-2-len(title)-1, 0)) + "╮"
	fmt.Println(brandBorder(top))
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		fmt.Printf("%s %s %s %s %s\n", ui.Brand("│"), center(l, leftW), ui.Brand("│"), pad(r, rightW), ui.Brand("│"))
	}
	fmt.Println(ui.Brand("╰" + strings.Repeat("─", total-2) + "╯"))
	fmt.Println()
}

func brandBorder(top string) string {
	if !ui.Color {
		return top
	}
	// The title is already colored; the rest of the border matches it
	return ui.Brand("╭─") + strings.TrimPrefix(top, "╭─")
}

// compactWelcome is for narrow terminals: Trace and one line of context.
func (s *session) compactWelcome(build Build) {
	for i, line := range mascot {
		fmt.Println(paintMascot(line, i))
	}
	fmt.Printf("%s %s\n", ui.Bold("TraceX CLI"), ui.Dim(build.Version))
	fmt.Println(ui.Dim(s.creds.Organization + " · " + s.project.Name))
	fmt.Println()
}

// tightest is the smallest "% left" of the plan's limits (-1: none).
func tightest(b *api.Billing) int {
	low := -1
	meters := []*api.Meter{&b.Usage.Investigations, b.Usage.ChatMessages}
	if b.Usage.AI != nil {
		meters = append(meters, &b.Usage.AI.TokensWindow)
	}
	for _, m := range meters {
		if left := m.Left(); left >= 0 && (low < 0 || left < low) {
			low = left
		}
	}
	return low
}

// printUsage shows the plan's limits as "% left", like the dashboard's menu.
func (s *session) printUsage(ctx context.Context) error {
	billing, err := s.client.Billing(ctx)
	if err != nil {
		return err
	}
	fmt.Println(ui.Dim("Usage source: " + billing.PlanName() + " plan"))
	row := func(label string, m *api.Meter, note string) {
		left := "Unlimited"
		if l := m.Left(); l >= 0 {
			left = fmt.Sprintf("%d%% left", l)
			switch {
			case l <= 10:
				left = ui.Red(left)
			case l <= 25:
				left = ui.Yellow(left)
			}
		}
		line := "  " + pad(label, 26) + left
		if note != "" {
			line += "  " + ui.Dim(note)
		}
		fmt.Println(line)
	}
	if ai := billing.Usage.AI; ai != nil {
		note := ""
		if ai.TokensFreeAt != nil {
			note = "resets " + ai.TokensFreeAt.Local().Format("3:04 pm")
		}
		row(fmt.Sprintf("%g-hour AI limit", ai.WindowHours), &ai.TokensWindow, note)
	}
	row("Monthly investigations", &billing.Usage.Investigations, "")
	if billing.Usage.ChatMessages != nil {
		row("Monthly questions", billing.Usage.ChatMessages, "")
	}
	fmt.Println(ui.Dim("  Manage usage: " + firstNonEmpty(s.creds.AppURL, "https://app.tracexpro.com") + "/billing"))
	return nil
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mustCwd() string {
	cwd, _ := os.Getwd()
	return cwd
}
