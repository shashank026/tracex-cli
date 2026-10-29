package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// --- sign-in -------------------------------------------------------------------------

// DeviceCode is a pending browser sign-in.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// User is the signed-in person.
type User struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	Name             string `json:"name"`
	Role             string `json:"role"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
}

// Session is what an approved sign-in returns.
type Session struct {
	Status      string `json:"status"` // "pending" until approved
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	User        User   `json:"user"`
}

func (c *Client) StartDevice(ctx context.Context, client string) (*DeviceCode, error) {
	var out DeviceCode
	return &out, c.JSON(ctx, "POST", "/v1/cli/device", map[string]string{"client": client}, &out)
}

func (c *Client) PollDevice(ctx context.Context, deviceCode string) (*Session, error) {
	var out Session
	return &out, c.JSON(ctx, "POST", "/v1/cli/token", map[string]string{"device_code": deviceCode}, &out)
}

func (c *Client) Logout(ctx context.Context) error {
	return c.JSON(ctx, "POST", "/v1/auth/logout", nil, nil)
}

// --- projects and repositories --------------------------------------------------------

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Connection struct {
	ID       string     `json:"connection_id"`
	Owner    string     `json:"owner"`
	Repo     string     `json:"repository"`
	Branch   string     `json:"branch"`
	Status   string     `json:"analysis_status"`
	Problem  string     `json:"analysis_error"`
	Analyzed *time.Time `json:"last_analyzed_at"`
}

type Installation struct {
	ID      int64  `json:"installation_id"`
	Account string `json:"account_login"`
}

type Repository struct {
	ID            int64  `json:"id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

type Branch struct {
	Name string `json:"name"`
}

func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	return out, c.JSON(ctx, "GET", "/v1/projects", nil, &out)
}

func (c *Client) CreateProject(ctx context.Context, name string) (*Project, error) {
	var out Project
	return &out, c.JSON(ctx, "POST", "/v1/projects", map[string]string{"name": name}, &out)
}

func (c *Client) Connections(ctx context.Context, projectID string) ([]Connection, error) {
	var out []Connection
	return out, c.JSON(ctx, "GET", "/v1/codebase-connections"+Query("project_id", projectID), nil, &out)
}

func (c *Client) Installations(ctx context.Context, projectID string) ([]Installation, error) {
	var out []Installation
	path := "/v1/codebase-connections/github/installations" + Query("project_id", projectID)
	return out, c.JSON(ctx, "GET", path, nil, &out)
}

func (c *Client) Repositories(ctx context.Context, installationID int64) ([]Repository, error) {
	var out []Repository
	path := fmt.Sprintf("/v1/codebase-connections/github/installations/%d/repositories", installationID)
	return out, c.JSON(ctx, "GET", path, nil, &out)
}

func (c *Client) Branches(ctx context.Context, installationID int64, owner, repo string) ([]Branch, error) {
	var out []Branch
	path := fmt.Sprintf("/v1/codebase-connections/github/installations/%d/repositories/%s/%s/branches",
		installationID, url.PathEscape(owner), url.PathEscape(repo))
	return out, c.JSON(ctx, "GET", path, nil, &out)
}

func (c *Client) InstallURL(ctx context.Context, projectID string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	path := "/v1/codebase-connections/github/install-url" + Query("project_id", projectID)
	return out.URL, c.JSON(ctx, "GET", path, nil, &out)
}

type ConnectRequest struct {
	ProjectID      string `json:"project_id"`
	InstallationID int64  `json:"installation_id"`
	RepositoryID   int64  `json:"repository_id"`
	Owner          string `json:"owner"`
	Repository     string `json:"repository"`
	Branch         string `json:"branch"`
}

func (c *Client) Connect(ctx context.Context, req ConnectRequest) (*Connection, error) {
	var out Connection
	return &out, c.JSON(ctx, "POST", "/v1/codebase-connections", req, &out)
}

// --- conversations ---------------------------------------------------------------------

// Step is one thing TraceX did while answering (a query, a code search...).
type Step struct {
	Phase   string `json:"phase"` // step_started | step_finished
	CallID  string `json:"call_id"`
	Tool    string `json:"tool"`
	Status  string `json:"status"`
	Done    string `json:"done"` // the status once finished ("Read auth.service.ts")
	Thought string `json:"thought"`
	Result  string `json:"result"`
	OK      *bool  `json:"ok"`
	MS      int    `json:"ms"`
}

// Message is a stored message of a conversation.
type Message struct {
	ID      string           `json:"id"`
	Role    string           `json:"role"`
	Content string           `json:"content"`
	Status  string           `json:"status"`
	Sources []map[string]any `json:"sources"`
	Error   string           `json:"error"`
}

// Opening is the first event of a streamed answer.
type Opening struct {
	Thread struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"thread"`
	Assistant Message `json:"assistant"`
}

// Ask starts a conversation (threadID "") or continues one, streaming the answer.
func (c *Client) Ask(ctx context.Context, projectID, threadID, question string, onEvent func(Event) error) error {
	if threadID == "" {
		body := map[string]string{"project_id": projectID, "content": question}
		return c.Stream(ctx, "/v1/chats", body, onEvent)
	}
	body := map[string]string{"content": question}
	return c.Stream(ctx, "/v1/chats/"+url.PathEscape(threadID)+"/messages", body, onEvent)
}

// Stop asks TraceX to stop an answer that is still running.
func (c *Client) Stop(ctx context.Context, threadID, messageID string) error {
	path := fmt.Sprintf("/v1/chats/%s/messages/%s/stop", url.PathEscape(threadID), url.PathEscape(messageID))
	return c.JSON(ctx, "POST", path, nil, nil)
}

// --- usage ---------------------------------------------------------------------------

// Meter is usage against a limit (Included nil: no limit).
type Meter struct {
	Used     float64  `json:"used"`
	Included *float64 `json:"included"`
}

// Left is the percent of the limit still available, or -1 without a limit.
func (m *Meter) Left() int {
	if m == nil || m.Included == nil || *m.Included <= 0 {
		return -1
	}
	left := 100 - int(m.Used / *m.Included * 100 + 0.5)
	return max(left, 0)
}

// Billing is the organization's plan and how much of it is used.
type Billing struct {
	CurrentPlan string `json:"current_plan"`
	Plans       []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"plans"`
	Usage struct {
		Investigations Meter  `json:"investigations"`
		ChatMessages   *Meter `json:"chat_messages"`
		AI             *struct {
			TokensWindow Meter      `json:"tokens_window"`
			WindowHours  float64    `json:"window_hours"`
			TokensFreeAt *time.Time `json:"tokens_free_at"`
		} `json:"ai"`
	} `json:"usage"`
}

// PlanName is the current plan's display name.
func (b *Billing) PlanName() string {
	for _, p := range b.Plans {
		if p.ID == b.CurrentPlan {
			return p.Name
		}
	}
	return b.CurrentPlan
}

func (c *Client) Billing(ctx context.Context) (*Billing, error) {
	var out Billing
	return &out, c.JSON(ctx, "GET", "/v1/organization/billing", nil, &out)
}

// Thread is one conversation in the project's list.
type Thread struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c *Client) Threads(ctx context.Context, projectID string) ([]Thread, error) {
	var out []Thread
	return out, c.JSON(ctx, "GET", "/v1/chats"+Query("project_id", projectID), nil, &out)
}

// --- coding mode -----------------------------------------------------------------------

// ToolCall is a tool the coding agent wants run.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is what running a tool returned.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// CodeTurn is one message of a coding conversation (user, assistant or tool).
type CodeTurn struct {
	Role        string            `json:"role"`
	Text        string            `json:"text,omitempty"`
	ToolCalls   []ToolCall        `json:"tool_calls,omitempty"`
	Results     []ToolResult      `json:"results,omitempty"`
	Raw         []json.RawMessage `json:"raw,omitempty"`
	RawProvider string            `json:"raw_provider,omitempty"`
}

// CodeReply is the model's next move: text, tools to run, or both.
type CodeReply struct {
	Text        string            `json:"text"`
	ToolCalls   []ToolCall        `json:"tool_calls"`
	Raw         []json.RawMessage `json:"raw"`
	RawProvider string            `json:"raw_provider"`
	StopReason  string            `json:"stop_reason"`
}

// CodeTurn asks the coding agent for its next step in a local repository.
func (c *Client) CodeTurn(ctx context.Context, turns []CodeTurn, context string) (*CodeReply, error) {
	var out CodeReply
	body := map[string]any{"turns": turns, "context": context}
	return &out, c.LongJSON(ctx, "POST", "/v1/cli/code/turn", body, &out)
}
