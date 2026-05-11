package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jan/git-backtrack/internal/tool"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []toolTextContent `json:"content"`
	IsError bool              `json:"isError,omitempty"`
}

func Serve(stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	encoder := json.NewEncoder(stdout)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = encoder.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		resp := handleRequest(req, stderr)
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleRequest(req request, stderr io.Writer) response {
	switch req.Method {
	case "initialize":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "git-backtrack", "version": "dev"},
		}}
	case "tools/list":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefinitions()}}
	case "tools/call":
		result, err := handleToolCall(req.Params, stderr)
		if err != nil {
			return response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
		}
		return response{JSONRPC: "2.0", ID: req.ID, Result: result}
	default:
		return response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
	}
}

func handleToolCall(params json.RawMessage, stderr io.Writer) (toolCallResult, error) {
	var call toolCallParams
	if err := json.Unmarshal(params, &call); err != nil {
		return toolCallResult{}, fmt.Errorf("invalid tools/call params: %w", err)
	}
	args := []string{}
	cleanup := func() {}
	defer cleanup()

	switch call.Name {
	case "git_backtrack_help":
		args = []string{"help", "--json"}
	case "git_backtrack_list":
		parsed, err := parseToolArguments(call.Arguments)
		if err != nil {
			return toolCallResult{}, err
		}
		args = []string{"list", "--path", parsed.stringDefault("path", "."), "--json"}
		if ref := parsed.stringDefault("ref", ""); ref != "" {
			args = append(args, "--ref", ref)
		}
	case "git_backtrack_validate":
		parsed, err := parseToolArguments(call.Arguments)
		if err != nil {
			return toolCallResult{}, err
		}
		planPath, remove, err := parsed.planPath()
		if err != nil {
			return toolCallResult{}, err
		}
		cleanup = remove
		args = []string{"validate", "--path", parsed.stringDefault("path", "."), "--plan", planPath, "--json"}
	case "git_backtrack_apply":
		parsed, err := parseToolArguments(call.Arguments)
		if err != nil {
			return toolCallResult{}, err
		}
		planPath, remove, err := parsed.planPath()
		if err != nil {
			return toolCallResult{}, err
		}
		cleanup = remove
		args = []string{"apply", "--path", parsed.stringDefault("path", "."), "--plan", planPath, "--json"}
		if parsed.boolDefault("yes", false) {
			args = append(args, "--yes")
		}
	default:
		return toolCallResult{}, fmt.Errorf("unknown tool %q", call.Name)
	}

	var stdout bytes.Buffer
	var capturedStderr bytes.Buffer
	status := tool.Run(args, &stdout, io.MultiWriter(stderr, &capturedStderr))
	text := strings.TrimSpace(stdout.String())
	if capturedStderr.Len() > 0 {
		text += "\n" + strings.TrimSpace(capturedStderr.String())
	}
	return toolCallResult{Content: []toolTextContent{{Type: "text", Text: text}}, IsError: status != 0}, nil
}

type parsedArgs map[string]json.RawMessage

func parseToolArguments(raw json.RawMessage) (parsedArgs, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return parsedArgs{}, nil
	}
	var parsed parsedArgs
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("arguments must be an object: %w", err)
	}
	return parsed, nil
}

func (a parsedArgs) stringDefault(name string, fallback string) string {
	raw, ok := a[name]
	if !ok {
		return fallback
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func (a parsedArgs) boolDefault(name string, fallback bool) bool {
	raw, ok := a[name]
	if !ok {
		return fallback
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func (a parsedArgs) planPath() (string, func(), error) {
	if path := a.stringDefault("plan_path", ""); path != "" {
		return path, func() {}, nil
	}
	raw, ok := a["plan"]
	if !ok {
		return "", func() {}, fmt.Errorf("plan or plan_path is required")
	}
	file, err := os.CreateTemp("", "git-backtrack-mcp-plan-*.json")
	if err != nil {
		return "", func() {}, err
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		_ = os.Remove(file.Name())
		return "", func() {}, err
	}
	return file.Name(), func() { _ = os.Remove(file.Name()) }, nil
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "git_backtrack_help",
			Description: "Return the machine-readable git-backtrack JSON tool contract.",
			InputSchema: objectSchema(nil, nil),
		},
		{
			Name:        "git_backtrack_list",
			Description: "List reachable commits for a repository/ref as JSON.",
			InputSchema: objectSchema(map[string]any{
				"path": stringSchema("Repository path. Defaults to current directory."),
				"ref":  stringSchema("Branch/ref to list. Defaults to HEAD."),
			}, nil),
		},
		{
			Name:        "git_backtrack_validate",
			Description: "Validate a git-backtrack rewrite plan. Accepts either plan object or plan_path.",
			InputSchema: objectSchema(map[string]any{
				"path":      stringSchema("Repository path. Defaults to current directory."),
				"plan_path": stringSchema("Path to a JSON plan file."),
				"plan":      map[string]any{"type": "object", "description": "Inline plan object."},
			}, nil),
		},
		{
			Name:        "git_backtrack_apply",
			Description: "Validate and apply a rewrite plan. Requires yes=true for history rewrites.",
			InputSchema: objectSchema(map[string]any{
				"path":      stringSchema("Repository path. Defaults to current directory."),
				"plan_path": stringSchema("Path to a JSON plan file."),
				"plan":      map[string]any{"type": "object", "description": "Inline plan object."},
				"yes":       map[string]any{"type": "boolean", "description": "Required to apply changes."},
			}, nil),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
