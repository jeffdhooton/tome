// Package mcp implements a minimal MCP stdio server that exposes tome's
// schema queries as tools Claude Code can call directly.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const protocolVersion = "2025-06-18"

// Dialer is the minimal interface over the tome daemon client.
type Dialer interface {
	Call(ctx context.Context, method string, params, out any) error
	Close() error
}

// DialFunc returns a fresh Dialer.
type DialFunc func() (Dialer, error)

// Server is a long-running MCP stdio server.
type Server struct {
	dial DialFunc
	mu   sync.Mutex
	out  *bufio.Writer
}

func New(dial DialFunc) *Server {
	return &Server{dial: dial}
}

// Serve runs the read-dispatch-write loop.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = bufio.NewWriter(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handleLine(ctx, line)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *Server) handleLine(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(nil, -32700, "parse error: "+err.Error(), nil)
		return
	}

	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		// No response needed.
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	case "ping":
		if !isNotification {
			s.writeResult(req.ID, map[string]any{})
		}
	default:
		if !isNotification {
			s.writeError(req.ID, -32601, "method not found: "+req.Method, nil)
		}
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) writeResult(id json.RawMessage, result any) {
	s.writeMessage(&response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id json.RawMessage, code int, msg string, data any) {
	s.writeMessage(&response{JSONRPC: "2.0", ID: id, Error: &responseError{Code: code, Message: msg, Data: data}})
}

func (s *Server) writeMessage(resp *response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resp.ID == nil {
		resp.ID = json.RawMessage(`null`)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tome mcp: marshal response: %v\n", err)
		return
	}
	_, _ = s.out.Write(b)
	_ = s.out.WriteByte('\n')
	_ = s.out.Flush()
}

// ---- initialize ----

type initializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

func (s *Server) handleInitialize(req request) {
	var p initializeParams
	_ = json.Unmarshal(req.Params, &p)

	version := p.ProtocolVersion
	if version == "" {
		version = protocolVersion
	}

	result := map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "tome",
			"version": "0.1.0",
		},
	}
	s.writeResult(req.ID, result)
}

// ---- tools/list ----

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

var toolDefinitions = []tool{
	{
		Name:        "tome_describe",
		Description: "Describe the columns, types, indexes, and foreign keys of a database table. Use this when you need to know a table's structure — replaces reading migrations, models, and factories. Returns the full schema for one table in one call.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"table":   map[string]any{"type": "string", "description": "The table name to describe."},
				"project": map[string]any{"type": "string", "description": "Absolute path to the project root. Defaults to cwd."},
			},
			"required": []string{"table"},
		}),
	},
	{
		Name:        "tome_relations",
		Description: "Find all tables that reference or are referenced by a target table through foreign keys. Use for 'what tables reference users', 'what does orders connect to', or any FK/relationship question.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"table":   map[string]any{"type": "string", "description": "The table name to find relationships for."},
				"project": map[string]any{"type": "string", "description": "Absolute path to the project root. Defaults to cwd."},
			},
			"required": []string{"table"},
		}),
	},
	{
		Name:        "tome_search",
		Description: "Search for tables or columns by name substring. Use when you need to discover schema elements — 'find tables matching email', 'which table has a status column'.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":   map[string]any{"type": "string", "description": "Search term to match against table and column names."},
				"project": map[string]any{"type": "string", "description": "Absolute path to the project root. Defaults to cwd."},
			},
			"required": []string{"query"},
		}),
	},
	{
		Name:        "tome_enums",
		Description: "List the allowed values for enum/set database columns. Use when you need to know valid values for a constrained column — 'what are the valid order statuses', 'what values can user.role have'.",
		InputSchema: mustMarshal(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"table":   map[string]any{"type": "string", "description": "Filter to enums from this table. Omit for all enums."},
				"column":  map[string]any{"type": "string", "description": "Filter to this specific column. Requires table."},
				"project": map[string]any{"type": "string", "description": "Absolute path to the project root. Defaults to cwd."},
			},
		}),
	},
}

func (s *Server) handleToolsList(req request) {
	s.writeResult(req.ID, map[string]any{"tools": toolDefinitions})
}

// ---- tools/call ----

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(ctx context.Context, req request) {
	var p toolsCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.writeError(req.ID, -32602, "invalid params: "+err.Error(), nil)
		return
	}

	switch p.Name {
	case "tome_describe":
		s.callDaemonTool(ctx, req.ID, "describe", p.Arguments, "tome_describe")
	case "tome_relations":
		s.callDaemonTool(ctx, req.ID, "relations", p.Arguments, "tome_relations")
	case "tome_search":
		s.callDaemonTool(ctx, req.ID, "search", p.Arguments, "tome_search")
	case "tome_enums":
		s.callDaemonTool(ctx, req.ID, "enums", p.Arguments, "tome_enums")
	default:
		s.writeToolError(req.ID, fmt.Sprintf("unknown tool %q", p.Name))
	}
}

func (s *Server) callDaemonTool(ctx context.Context, id json.RawMessage, method string, rawArgs json.RawMessage, toolName string) {
	start := time.Now()

	// Parse arguments and inject project if missing.
	var args map[string]any
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		s.writeToolError(id, "invalid arguments: "+err.Error())
		return
	}
	if _, ok := args["project"]; !ok {
		cwd, err := os.Getwd()
		if err != nil {
			s.writeToolError(id, "resolve cwd: "+err.Error())
			return
		}
		args["project"] = cwd
	}

	client, err := s.dial()
	if err != nil {
		logCall(toolName, start, err)
		s.writeToolError(id, "dial tome daemon: "+err.Error())
		return
	}
	defer client.Close()

	var raw json.RawMessage
	if err := client.Call(ctx, method, args, &raw); err != nil {
		logCall(toolName, start, err)
		s.writeToolError(id, "tome "+method+": "+err.Error())
		return
	}

	logCall(toolName, start, nil)
	s.writeToolResult(id, prettyJSON(raw), false)
}

func (s *Server) writeToolResult(id json.RawMessage, text string, isError bool) {
	s.writeResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	})
}

func (s *Server) writeToolError(id json.RawMessage, msg string) {
	s.writeToolResult(id, msg, true)
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// logCall appends a line to ~/.tome/logs/mcp-calls.jsonl.
func logCall(tool string, start time.Time, callErr error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".tome", "logs")
	_ = os.MkdirAll(dir, 0o755)

	f, err := os.OpenFile(filepath.Join(dir, "mcp-calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	entry := map[string]any{
		"ts":         start.UTC().Format(time.RFC3339),
		"tool":       tool,
		"latency_ms": time.Since(start).Milliseconds(),
	}
	if callErr != nil {
		entry["error"] = callErr.Error()
	}
	b, _ := json.Marshal(entry)
	fmt.Fprintf(f, "%s\n", b)
}
