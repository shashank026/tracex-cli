// Package api talks to the TraceX API (/v1), the same one the dashboard uses.
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// Client calls one TraceX server, signed in or not.
type Client struct {
	BaseURL string
	Token   string
	Version string
	HTTP    *http.Client
}

// New returns a client for baseURL.
func New(baseURL, token, version string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Version: version,
		// No overall timeout: answers stream for as long as they take
		HTTP: &http.Client{Transport: http.DefaultTransport},
	}
}

// Error is a response TraceX refused, with its own explanation.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string { return e.Detail }

// ErrUnauthorized means the session ended or was revoked.
var ErrUnauthorized = errors.New("your sign-in ended: run `tracex login`")

// UserAgent names the CLI in the dashboard's session list.
func (c *Client) UserAgent() string {
	return fmt.Sprintf("TraceX-CLI/%s (%s; %s)", strings.TrimPrefix(c.Version, "v"), runtime.GOOS, runtime.GOARCH)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", c.UserAgent())
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("cannot reach TraceX at %s: %w", c.BaseURL, err)
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && c.Token != "" {
		return nil, ErrUnauthorized
	}
	return nil, &Error{Status: resp.StatusCode, Detail: detail(resp)}
}

// detail is the "detail" (FastAPI) or "message" of an error response.
func detail(resp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body struct {
		Detail  any    `json:"detail"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		switch d := body.Detail.(type) {
		case string:
			return d
		case []any:
			if len(d) > 0 {
				if item, ok := d[0].(map[string]any); ok {
					if msg, ok := item["msg"].(string); ok {
						return msg
					}
				}
			}
		}
		if body.Message != "" {
			return body.Message
		}
	}
	return fmt.Sprintf("TraceX returned HTTP %d", resp.StatusCode)
}

// JSON sends body (if any) and decodes the response into out (if any).
func (c *Client) JSON(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Event is one server-sent event of a streamed answer.
type Event struct {
	Name string
	Data json.RawMessage
}

// Stream posts body and calls onEvent for each event until the stream ends.
func (c *Client) Stream(ctx context.Context, path string, body any, onEvent func(Event) error) error {
	req, err := c.newRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return ReadEvents(resp.Body, onEvent)
}

// ReadEvents parses a text/event-stream body.
func ReadEvents(body io.Reader, onEvent func(Event) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	var name string
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if name != "" || len(data) > 0 {
				event := Event{Name: name, Data: json.RawMessage(strings.Join(data, "\n"))}
				if event.Name == "" {
					event.Name = "message"
				}
				if err := onEvent(event); err != nil {
					return err
				}
			}
			name, data = "", nil
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return scanner.Err()
}

// Query builds "?a=b" from pairs.
func Query(pairs ...string) string {
	values := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values.Set(pairs[i], pairs[i+1])
	}
	return "?" + values.Encode()
}
