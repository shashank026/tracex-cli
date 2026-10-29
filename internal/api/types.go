package api

import (
	"context"
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
