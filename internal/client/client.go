// Package client is a minimal MCP (Streamable HTTP) client used to call the
// same tools the Quiver MCP server exposes. The CLI is deliberately a thin,
// deterministic wrapper: every command is one tools/call.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/built-for-devs/quiver/internal/apperr"
)

const protocolVersion = "2025-06-18"

// Client talks JSON-RPC to an MCP endpoint.
type Client struct {
	URL       string
	Token     string
	UserAgent string
	HTTP      *http.Client

	mu         sync.Mutex
	nextID     int
	sessionID  string
	server     *ServerInfo
	initDone   bool
	negotiated string
}

// New returns a client for the MCP endpoint at url.
func New(url, token, userAgent string, timeout time.Duration) *Client {
	return &Client{
		URL:       url,
		Token:     token,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: timeout},
	}
}

// ServerInfo is returned by the initialize handshake.
type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
}

// Content is one block of a tool result.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is the result of tools/call.
type ToolResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Text joins all text content blocks.
func (r *ToolResult) Text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Data returns the result as JSON: structuredContent if present, otherwise the
// text content if it parses as JSON. ok is false for plain-text results.
func (r *ToolResult) Data() (json.RawMessage, bool) {
	if len(r.StructuredContent) > 0 && string(r.StructuredContent) != "null" {
		return r.StructuredContent, true
	}
	t := strings.TrimSpace(r.Text())
	if t != "" && json.Valid([]byte(t)) {
		return json.RawMessage(t), true
	}
	return nil, false
}

// Tool describes a tool from tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// Initialize performs the MCP handshake. Safe to call repeatedly; it runs once.
func (c *Client) Initialize(ctx context.Context) (*ServerInfo, error) {
	c.mu.Lock()
	done := c.initDone
	c.mu.Unlock()
	if done {
		return c.server, nil
	}

	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "quiver-cli", "version": c.UserAgent},
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}
	var res struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, apperr.Wrap(apperr.CodeGeneral, err, "decode initialize result")
	}
	res.ServerInfo.ProtocolVersion = res.ProtocolVersion

	c.mu.Lock()
	c.server = &res.ServerInfo
	c.negotiated = res.ProtocolVersion
	c.initDone = true
	c.mu.Unlock()

	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return nil, err
	}
	return c.server, nil
}

// ListTools returns every tool the server exposes, following pagination.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	var all []Tool
	cursor := ""
	for {
		var params map[string]any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, apperr.Wrap(apperr.CodeGeneral, err, "decode tools/list result")
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes a tool. A result with isError set is returned as a typed
// *apperr.Error classified from the message text.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res ToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, apperr.Wrap(apperr.CodeGeneral, err, "decode tools/call result")
	}
	if res.IsError {
		msg := strings.TrimSpace(res.Text())
		if msg == "" {
			msg = "tool " + name + " failed"
		}
		return &res, &apperr.Error{Code: classify(msg), Msg: msg}
	}
	return &res, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	resp, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}, id)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, rpcErr(method, resp.Error)
	}
	return resp.Result, nil
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	_, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params}, 0)
	return err
}

// post sends one JSON-RPC message. For requests (wantID > 0) it returns the
// matching response from either a JSON body or an SSE stream.
func (c *Client) post(ctx context.Context, msg rpcRequest, wantID int) (*rpcResponse, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeUsage, err, "invalid api_url")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", "quiver-cli/"+c.UserAgent)
	c.mu.Lock()
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	if c.negotiated != "" {
		req.Header.Set("MCP-Protocol-Version", c.negotiated)
	}
	c.mu.Unlock()

	res, err := c.HTTP.Do(req)
	if err != nil {
		if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
			return nil, apperr.Wrap(apperr.CodeUnavailable, err, "request timed out")
		}
		return nil, apperr.Wrap(apperr.CodeUnavailable, err, "request failed")
	}
	defer res.Body.Close()

	if sid := res.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	if err := statusErr(res); err != nil {
		return nil, err
	}
	if wantID == 0 {
		io.Copy(io.Discard, res.Body)
		return nil, nil
	}

	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if mt == "text/event-stream" {
		return readSSE(res.Body, wantID)
	}
	var out rpcResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, apperr.Wrap(apperr.CodeGeneral, err, "decode response")
	}
	return &out, nil
}

// readSSE scans an event stream for the response whose id matches wantID.
// Server-initiated notifications and requests on the stream are ignored.
func readSSE(r io.Reader, wantID int) (*rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var data strings.Builder
	flush := func() (*rpcResponse, bool) {
		defer data.Reset()
		if data.Len() == 0 {
			return nil, false
		}
		var msg rpcResponse
		if json.Unmarshal([]byte(data.String()), &msg) != nil {
			return nil, false
		}
		if msg.ID != nil && *msg.ID == wantID && (msg.Result != nil || msg.Error != nil) {
			return &msg, true
		}
		return nil, false
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if m, ok := flush(); ok {
				return m, nil
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if m, ok := flush(); ok {
		return m, nil
	}
	if err := sc.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeUnavailable, err, "read event stream")
	}
	return nil, apperr.Unavailable("event stream closed before response %d arrived", wantID)
}

func statusErr(res *http.Response) error {
	if res.StatusCode < 300 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	detail := strings.TrimSpace(string(b))
	msg := fmt.Sprintf("server returned %s", res.Status)
	if detail != "" {
		msg += ": " + detail
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return apperr.Auth("%s (check your token: quiver auth login)", msg)
	case res.StatusCode == http.StatusForbidden:
		return apperr.Auth("%s (token may be missing the mcp scope)", msg)
	case res.StatusCode == http.StatusNotFound:
		return apperr.NotFound("%s (check api_url)", msg)
	case res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnprocessableEntity:
		return apperr.Validation("%s", msg)
	case res.StatusCode >= 500:
		return apperr.Unavailable("%s", msg)
	default:
		return apperr.New(apperr.CodeGeneral, msg)
	}
}

func rpcErr(method string, e *rpcError) error {
	msg := fmt.Sprintf("%s: %s", method, e.Message)
	switch e.Code {
	case -32602: // invalid params
		return apperr.Validation("%s", msg)
	case -32601: // method not found (e.g. unknown tool)
		return apperr.NotFound("%s", msg)
	default:
		return &apperr.Error{Code: classify(e.Message), Msg: msg}
	}
}

// classify maps a free-text tool error to an exit code. Heuristic by
// necessity: MCP tool errors are text, not typed.
func classify(msg string) int {
	m := strings.ToLower(msg)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(m, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("not found", "no such", "does not exist", "unknown tool"):
		return apperr.CodeNotFound
	case has("unauthorized", "unauthenticated", "forbidden", "permission denied", "insufficient scope", "invalid token"):
		return apperr.CodeAuth
	case has("invalid", "required", "validation", "must be", "malformed"):
		return apperr.CodeValidation
	default:
		return apperr.CodeGeneral
	}
}
